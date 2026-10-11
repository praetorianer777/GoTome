-- What the plugin reads in GOtome's answers, kept apart from KOReader so
-- that it can be tested without it: the books of a device's OPDS feed, the
-- file each is saved as, and where a link of the feed points.
local M = {}

local entities = { lt = "<", gt = ">", quot = '"', apos = "'" }

local function unescape(s)
    s = s:gsub("&(%a+);", function(name) return entities[name] end)
    s = s:gsub("&#(%d+);", function(n) return string.char(tonumber(n) % 256) end)
    return (s:gsub("&amp;", "&"))
end

local function attributes(tag)
    local out = {}
    for name, value in tag:gmatch('([%w:_-]+)="([^"]*)"') do
        out[name] = unescape(value)
    end
    return out
end

local acquisition = "http://opds-spec.org/acquisition"

--- The books of an OPDS acquisition feed, in its order: their ID, title,
--- and download links, the best format first as GOtome lists them.
function M.entries(xml)
    local out = {}
    for body in xml:gmatch("<entry>(.-)</entry>") do
        local entry = {
            id = unescape(body:match("<id>(.-)</id>") or ""),
            title = unescape(body:match("<title>(.-)</title>") or ""),
            links = {},
        }
        for tag in body:gmatch("<link%s([^>]*)>") do
            local a = attributes(tag)
            if a.rel == acquisition and a.href then
                table.insert(entry.links, { href = a.href, type = a.type, format = a.title })
            end
        end
        table.insert(out, entry)
    end
    return out
end

local extensions = {
    ["application/epub+zip"] = "epub",
    ["application/pdf"] = "pdf",
    ["application/x-mobipocket-ebook"] = "mobi",
}

--- The extension a link's file is saved with: its format as GOtome names
--- it, or what its media type says.
function M.extension(link)
    if link.format and link.format:match("^%w+$") then
        return link.format:lower()
    end
    return extensions[link.type or ""] or "epub"
end

--- A file name for a book's title on any file system: no folders, no
--- characters FAT refuses, at most 80 bytes before the extension.
function M.file_name(title, extension)
    local name = title:gsub("[%c/\\:%*%?\"<>|]", "_"):gsub("^[%s%.]+", ""):gsub("[%s%.]+$", "")
    if #name > 80 then
        -- Cut before the character the 81st byte belongs to, not in it.
        local cut = 80
        while cut > 0 and name:byte(cut + 1) >= 0x80 and name:byte(cut + 1) < 0xC0 do
            cut = cut - 1
        end
        name = name:sub(1, cut)
    end
    if name == "" then
        name = "book"
    end
    return name .. "." .. extension
end

--- A file in the folder that is not there yet: the name, or the name with
--- a number before its extension. exists says whether a path is taken.
function M.free_path(folder, name, exists)
    local path = folder .. "/" .. name
    local stem, extension = name:match("^(.*)%.([^.]+)$")
    local n = 2
    while exists(path) do
        path = folder .. "/" .. stem .. " (" .. n .. ")." .. extension
        n = n + 1
    end
    return path
end

--- The address of a link the feed gives as a path on the server.
function M.absolute(server, href)
    if href:match("^https?://") then
        return href
    end
    local origin = server:match("^(https?://[^/]+)") or server
    return origin .. href
end

--- The address of the device's feed on the server the person entered,
--- which may end in a slash or name the catalogue already.
function M.feed_url(server)
    local base = server:gsub("/+$", ""):gsub("/api/v1/opds$", ""):gsub("/api/v1$", "")
    return base .. "/api/v1/opds/device"
end

return M
