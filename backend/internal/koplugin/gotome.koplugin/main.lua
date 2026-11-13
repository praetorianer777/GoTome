--[[--
GOtome for KOReader: fetches the books sent to this device from GOtome's
web app, when Wi-Fi comes up and from its menu, into a folder of the
person's choosing. It reads the device's own OPDS feed, signed in with the
person's user name and a device token from their GOtome profile;
downloading a book there marks it delivered, so each arrives once. It also
sends the highlights and notes of the book being read, from KOReader's own
record of them, so that they are in the person's GOtome library too.
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

local annotations = require("gotome_annotations")
local feed = require("gotome_feed")

-- The addresses of the server, the one the plugin was downloaded from
-- first, written into it by GOtome.
local ok_server, downloaded_from = pcall(require, "gotome_server")

-- How long an address may take to answer before the next is tried.
local PROBE_BLOCK_TIMEOUT, PROBE_TOTAL_TIMEOUT = 4, 8

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
        return feed.candidates(nil, downloaded_from)[1]
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

-- One request to the server, its body into sink: the HTTP status, or nil
-- and why not. body, if given, is sent as JSON.
function GOtome:request(method, url, sink, probe, body)
    local http = url:match("^https:") and require("ssl.https") or require("socket.http")
    if probe then
        socketutil:set_timeout(PROBE_BLOCK_TIMEOUT, PROBE_TOTAL_TIMEOUT)
    else
        socketutil:set_timeout(socketutil.LARGE_BLOCK_TIMEOUT, socketutil.LARGE_TOTAL_TIMEOUT)
    end
    local headers = {
        ["Authorization"] = "Basic " .. mime.b64(self:setting("user") .. ":" .. self:setting("token")),
        ["Accept"] = body and "application/json" or "application/atom+xml, */*",
    }
    if body then
        headers["Content-Type"] = "application/json"
        headers["Content-Length"] = tostring(#body)
    end
    local ok, result, code = pcall(http.request, {
        url = url,
        method = method,
        headers = headers,
        source = body and ltn12.source.string(body) or nil,
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

function GOtome:get(url, sink, probe)
    return self:request("GET", url, sink, probe)
end

--- The address of the server that answers now: the one that worked last
--- time is tried first, then the others in order. Any answer counts, a
--- wrong token too, so that it is told rather than another address tried.
function GOtome:reach()
    local own = self.settings:readSetting("server")
    local addresses = feed.candidates(own, ok_server and downloaded_from or nil,
        self.settings:readSetting("last_good"))
    local address = feed.choose(addresses, function(candidate)
        local code = self:get(feed.feed_url(candidate), ltn12.sink.null(), true)
        return code ~= nil
    end)
    if address then
        if address ~= self.settings:readSetting("last_good") then
            self:save("last_good", address)
        end
        return address
    end
    return addresses[1]
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
    local server = self:reach()
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
    if self:setting("send_highlights") ~= false and self:ready() and self.ui.document then
        UIManager:scheduleIn(4, function() self:sendHighlights(true) end)
    end
end

--- A name for this KOReader that stays the same, so that GOtome knows which
--- highlights are this device's to replace.
function GOtome:client()
    local id = self.settings:readSetting("client_id")
    if not id then
        math.randomseed(os.time() + math.floor(os.clock() * 1000000))
        id = ("%08x%04x"):format(math.random(0, 0x7fffffff), math.random(0, 0xffff))
        self:save("client_id", id)
    end
    return "koreader:" .. id
end

--- KOReader's record of the open book's highlights and notes in the shape of
--- its sidecar. What is in memory comes first, as the file is written only
--- now and then; the older versions' tables are the fallback.
function GOtome:sidecar()
    local ui = self.ui
    local settings = ui.doc_settings
    local record = {
        annotations = ui.annotation and ui.annotation.annotations or settings:readSetting("annotations"),
        highlight = settings:readSetting("highlight"),
        bookmarks = settings:readSetting("bookmarks") or (ui.bookmark and ui.bookmark.bookmarks),
        highlight_color = settings:readSetting("highlight_color"),
        doc_pages = settings:readSetting("doc_pages") or ui.document:getPageCount(),
        partial_md5_checksum = settings:readSetting("partial_md5_checksum"),
    }
    if not record.partial_md5_checksum then
        local ok, md5 = pcall(function() return ui.document:fastDigest(settings) end)
        record.partial_md5_checksum = ok and md5 or nil
    end
    return record
end

--- Sends the highlights and notes of the open book. Quiet, it says nothing
--- unless something is wrong with the sign-in. GOtome learns of those the
--- book no longer has from the full set, so a book is sent as it stands.
function GOtome:sendHighlights(quiet)
    if not self.ui.document or not self.ui.doc_settings or not self:ready() then
        return
    end
    local record = self:sidecar()
    local md5 = annotations.document(record)
    if not md5 then
        return
    end
    local page_size
    if self.ui.paging then
        page_size = function(page)
            local ok, size = pcall(function() return self.ui.document:getPageDimensions(page, 1, 0) end)
            return ok and size and { w = size.w, h = size.h } or nil
        end
    end
    local body = annotations.body(record, self:client(), page_size)
    local server = self:setting("last_good") or self:setting("server")
    local url = feed.api_url(server, "/device/annotations/" .. md5)
    local response = {}
    local code, err = self:request("PUT", url, ltn12.sink.table(response), false, body)
    if code == 200 then
        if not quiet then
            UIManager:show(InfoMessage:new{ text = _("Highlights sent to GOtome."), timeout = 3 })
        end
    elseif code == 404 then
        if not quiet then
            UIManager:show(InfoMessage:new{ text = _("GOtome has no book with this file."), timeout = 4 })
        end
    else
        logger.warn("GOtome: sending highlights", code, err)
        if not quiet then
            UIManager:show(InfoMessage:new{ text = T(_("GOtome did not take the highlights: %1"), err or code) })
        end
    end
end

function GOtome:onCloseDocument()
    if self:setting("send_highlights") ~= false and self:ready() and NetworkMgr:isConnected() then
        self:sendHighlights(true)
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
                text = _("Send this book's highlights now"),
                enabled_func = function() return self.ui.document ~= nil and self:ready() end,
                callback = function()
                    NetworkMgr:runWhenOnline(function() self:sendHighlights(false) end)
                end,
            },
            {
                text = _("Send highlights when a book is closed"),
                checked_func = function() return self:setting("send_highlights") ~= false end,
                callback = function() self:save("send_highlights", self:setting("send_highlights") == false) end,
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
