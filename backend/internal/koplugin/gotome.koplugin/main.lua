--[[--
GOtome for KOReader: fetches the books sent to this device from GOtome's
web app, when Wi-Fi comes up and from its menu, into a folder of the
person's choosing. It reads the device's own OPDS feed, signed in with the
person's user name and a device token from their GOtome profile;
downloading a book there marks it delivered, so each arrives once.
]]

local DataStorage = require("datastorage")
local InfoMessage = require("ui/widget/infomessage")
local InputDialog = require("ui/widget/inputdialog")
local LuaSettings = require("luasettings")
local NetworkMgr = require("ui/network/manager")
local PathChooser = require("ui/widget/pathchooser")
local UIManager = require("ui/uimanager")
local WidgetContainer = require("ui/widget/container/widgetcontainer")
local lfs = require("libs/libkoreader-lfs")
local logger = require("logger")
local ltn12 = require("ltn12")
local mime = require("mime")
local socketutil = require("socketutil")
local T = require("ffi/util").template
local _ = require("gettext")

local feed = require("gotome_feed")

-- The address the plugin was downloaded from, written into it by GOtome.
local ok_server, downloaded_from = pcall(require, "gotome_server")

local GOtome = WidgetContainer:extend{
    name = "gotome",
    is_doc_only = false,
}

function GOtome:init()
    self.settings = LuaSettings:open(DataStorage:getSettingsDir() .. "/gotome.lua")
    self.ui.menu:registerToMainMenu(self)
end

function GOtome:setting(key)
    local value = self.settings:readSetting(key)
    if value == nil and key == "server" and ok_server then
        return downloaded_from
    end
    if value == nil and key == "folder" then
        return G_reader_settings:readSetting("home_dir") or DataStorage:getDataDir()
    end
    return value
end

function GOtome:save(key, value)
    self.settings:saveSetting(key, value)
    self.settings:flush()
end

function GOtome:ready()
    return (self:setting("server") or "") ~= "" and (self:setting("user") or "") ~= ""
        and (self:setting("token") or "") ~= ""
end

-- One GET of the server, its body into sink: the HTTP status, or nil and
-- why not.
function GOtome:get(url, sink)
    local http = url:match("^https:") and require("ssl.https") or require("socket.http")
    socketutil:set_timeout(socketutil.LARGE_BLOCK_TIMEOUT, socketutil.LARGE_TOTAL_TIMEOUT)
    local ok, result, code = pcall(http.request, {
        url = url,
        method = "GET",
        headers = {
            ["Authorization"] = "Basic " .. mime.b64(self:setting("user") .. ":" .. self:setting("token")),
            ["Accept"] = "application/atom+xml, */*",
        },
        sink = sink,
    })
    socketutil:reset_timeout()
    if not ok then
        return nil, tostring(result)
    end
    if result == nil then
        return nil, tostring(code)
    end
    return code
end

local function exists(path)
    return lfs.attributes(path, "mode") ~= nil
end

--- Fetches what waits for this device. Quiet, it says nothing when there
--- is nothing new, as when Wi-Fi comes up.
function GOtome:fetch(quiet)
    if not self:ready() then
        if not quiet then
            UIManager:show(InfoMessage:new{ text = _("Set up the server, your user name and the device token first.") })
        end
        return
    end
    local server = self:setting("server")
    local body = {}
    local code, err = self:get(feed.feed_url(server), ltn12.sink.table(body))
    if code ~= 200 then
        logger.warn("GOtome: the device's feed", code, err)
        local text = code == 401 and _("GOtome does not know this user name and device token.")
            or T(_("GOtome did not answer: %1"), err or code)
        UIManager:show(InfoMessage:new{ text = text })
        return
    end
    local folder = self:setting("folder")
    local fetched, failed = {}, 0
    for _i, entry in ipairs(feed.entries(table.concat(body))) do
        local link = entry.links[1]
        if link then
            local part = folder .. "/.gotome.part"
            local file = io.open(part, "wb")
            local got = file and self:get(feed.absolute(server, link.href), ltn12.sink.file(file))
            if got == 200 then
                local path = feed.free_path(folder, feed.file_name(entry.title, feed.extension(link)), exists)
                os.rename(part, path)
                table.insert(fetched, entry.title)
            else
                os.remove(part)
                failed = failed + 1
                logger.warn("GOtome: downloading", entry.title, got)
            end
        end
    end
    if #fetched > 0 then
        local FileManager = require("apps/filemanager/filemanager")
        if FileManager.instance then
            FileManager.instance:onRefresh()
        end
    end
    if #fetched > 0 or failed > 0 or not quiet then
        local text = #fetched == 0 and _("Nothing new from GOtome.")
            or T(_("From GOtome: %1"), table.concat(fetched, ", "))
        if failed > 0 then
            text = text .. "\n" .. T(_("%1 could not be downloaded; they are tried again next time."), failed)
        end
        UIManager:show(InfoMessage:new{ text = text, timeout = 5 })
    end
end

function GOtome:onNetworkConnected()
    if self:setting("on_wifi") ~= false and self:ready() then
        UIManager:scheduleIn(2, function() self:fetch(true) end)
    end
end

function GOtome:ask(title, key, hidden)
    local dialog
    dialog = InputDialog:new{
        title = title,
        input = hidden and "" or (self:setting(key) or ""),
        text_type = hidden and "password" or nil,
        buttons = { {
            { text = _("Cancel"), id = "close", callback = function() UIManager:close(dialog) end },
            { text = _("Save"), is_enter_default = true, callback = function()
                local value = dialog:getInputText():gsub("^%s+", ""):gsub("%s+$", "")
                self:save(key, value)
                UIManager:close(dialog)
            end },
        } },
    }
    UIManager:show(dialog)
    dialog:onShowKeyboard()
end

function GOtome:addToMainMenu(menu_items)
    menu_items.gotome = {
        text = _("GOtome"),
        sub_item_table = {
            {
                text = _("Fetch books sent here"),
                callback = function()
                    NetworkMgr:runWhenOnline(function() self:fetch(false) end)
                end,
            },
            {
                text = _("Fetch when Wi-Fi comes up"),
                checked_func = function() return self:setting("on_wifi") ~= false end,
                callback = function() self:save("on_wifi", self:setting("on_wifi") == false) end,
                separator = true,
            },
            {
                text_func = function() return T(_("Server: %1"), self:setting("server") or "") end,
                keep_menu_open = true,
                callback = function() self:ask(_("GOtome's address"), "server") end,
            },
            {
                text_func = function() return T(_("User name: %1"), self:setting("user") or "") end,
                keep_menu_open = true,
                callback = function() self:ask(_("Your user name in GOtome"), "user") end,
            },
            {
                text_func = function()
                    return (self:setting("token") or "") ~= "" and _("Device token: set") or _("Device token: not set")
                end,
                keep_menu_open = true,
                callback = function() self:ask(_("The device token from your GOtome profile"), "token", true) end,
            },
            {
                text_func = function() return T(_("Folder: %1"), self:setting("folder")) end,
                keep_menu_open = true,
                callback = function()
                    UIManager:show(PathChooser:new{
                        select_directory = true,
                        select_file = false,
                        path = self:setting("folder"),
                        onConfirm = function(path) self:save("folder", path) end,
                    })
                end,
            },
        },
    }
end

return GOtome
