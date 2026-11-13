-- What the plugin sends of a book's highlights and notes, kept apart from
-- KOReader so that it can be tested without it: it reads KOReader's own
-- record of them, the `annotations` list of its sidecar (metadata.<ext>.lua)
-- or the `highlight` and `bookmarks` tables older versions kept, and turns
-- them into what GOtome's PUT /device/annotations/<md5> takes.
local M = {}

local MAX_TEXT = 19000

local function text(value)
    if type(value) ~= "string" then
        return nil
    end
    value = value:gsub("^%s+", ""):gsub("%s+$", "")
    if value == "" then
        return nil
    end
    if #value > MAX_TEXT then
        local cut = MAX_TEXT
        while cut > 0 and value:byte(cut + 1) >= 0x80 and value:byte(cut + 1) < 0xC0 do
            cut = cut - 1
        end
        value = value:sub(1, cut)
    end
    return value
end

-- A place as KOReader writes it: the XPointer of a reflowable document, or
-- a point on a page of a fixed one.
local function place(pos)
    if type(pos) == "string" then
        return pos
    end
    if type(pos) == "table" and pos.page then
        return ("page:%d:%s:%s"):format(pos.page, tostring(pos.x or 0), tostring(pos.y or 0))
    end
    return nil
end

local function clamp(value)
    return math.min(math.max(value, 0), 1)
end

-- The rectangles of a highlight on a fixed-layout page as fractions of the
-- page from its top left; none where the page's size is not known.
local function rects(boxes, size)
    if type(boxes) ~= "table" or not size or not size.w or not size.h or size.w <= 0 or size.h <= 0 then
        return nil
    end
    local out = {}
    for _i, box in ipairs(boxes) do
        if type(box.x) == "number" and type(box.y) == "number" and type(box.w) == "number" and type(box.h) == "number" then
            table.insert(out, {
                x = clamp(box.x / size.w), y = clamp(box.y / size.h),
                w = clamp(box.w / size.w), h = clamp(box.h / size.h),
            })
        end
        if #out >= 200 then
            break
        end
    end
    return #out > 0 and out or nil
end

-- One highlight of either format as the server takes it, or nil where it
-- is no highlight (a plain bookmark has no passage).
local function convert(entry, sidecar, page_size)
    local passage = text(entry.text)
    local start = place(entry.pos0)
    if not passage or not start then
        return nil
    end
    local stop = place(entry.pos1)
    local paged = type(entry.pos0) == "table"
    local page = tonumber(type(entry.page) == "number" and entry.page or (paged and entry.pos0.page) or nil)
    local out = {
        key = table.concat({ entry.datetime or "", start, stop or "" }, "|"),
        kind = paged and "pdf" or "xpointer",
        locator = start,
        locator_end = stop,
        passage = passage,
        note = text(entry.note),
        color = entry.color or "yellow",
        chapter = text(entry.chapter),
        created_at = entry.datetime,
        updated_at = entry.datetime_updated,
    }
    if paged then
        out.locator = "page:" .. tostring(page)
        out.locator_end = nil
        out.page = page
        out.rects = rects(entry.pboxes, page and page_size and page_size(page))
    end
    local pages = tonumber(sidecar.doc_pages)
    local at = paged and page or tonumber(entry.pageno)
    if pages and pages > 0 and at and at > 0 then
        out.fraction = clamp(at / pages)
    end
    return out
end

-- The notes older KOReader kept in bookmarks, by the highlight they belong
-- to: the bookmark's `text` is the person's note where `notes` holds the
-- highlighted passage.
local function old_notes(sidecar)
    local by_time, by_start = {}, {}
    for _i, mark in ipairs(sidecar.bookmarks or {}) do
        local note = text(mark.text)
        if note and mark.highlighted ~= false and note ~= text(mark.notes) then
            if mark.datetime then by_time[mark.datetime] = note end
            local start = place(mark.pos0)
            if start then by_start[start] = note end
        end
    end
    return by_time, by_start
end

--- The highlights and notes of a sidecar (the table KOReader keeps in it),
--- in the order the document runs, as the server takes them. page_size(page)
--- says how big a page of a fixed-layout document is, {w =, h =}; it may be
--- left out.
function M.collect(sidecar, page_size)
    local out = {}
    if type(sidecar.annotations) == "table" and #sidecar.annotations > 0 then
        for _i, entry in ipairs(sidecar.annotations) do
            local item = convert(entry, sidecar, page_size)
            if item then table.insert(out, item) end
        end
    else
        local by_time, by_start = old_notes(sidecar)
        local pages = {}
        for page in pairs(sidecar.highlight or {}) do
            table.insert(pages, page)
        end
        table.sort(pages, function(a, b)
            if type(a) == type(b) then return a < b end
            return tostring(a) < tostring(b)
        end)
        for _i, page in ipairs(pages) do
            for _j, entry in ipairs(sidecar.highlight[page]) do
                local copy = {}
                for k, v in pairs(entry) do copy[k] = v end
                copy.page = copy.page or (type(page) == "number" and page or nil)
                copy.pageno = copy.pageno or (type(page) == "number" and page or nil)
                copy.note = copy.note or by_time[entry.datetime or ""] or by_start[place(entry.pos0) or ""]
                copy.color = copy.color or sidecar.highlight_color
                local item = convert(copy, sidecar, page_size)
                if item then table.insert(out, item) end
            end
        end
    end
    -- The same highlight twice (a copy in both forms) is one.
    local seen, unique = {}, {}
    for _i, item in ipairs(out) do
        if not seen[item.key] then
            seen[item.key] = true
            table.insert(unique, item)
        end
    end
    return unique
end

--- The name KOReader's progress sync gives the document, which GOtome
--- finds the book by.
function M.document(sidecar)
    local md5 = sidecar.partial_md5_checksum
    return type(md5) == "string" and md5:match("^%x+$") and md5:lower() or nil
end

local escapes = { ['"'] = '\\"', ["\\"] = "\\\\", ["\n"] = "\\n", ["\r"] = "\\r", ["\t"] = "\\t" }

local function quote(s)
    return '"' .. s:gsub('[%c"\\]', function(c)
        return escapes[c] or ("\\u%04x"):format(c:byte())
    end) .. '"'
end

local array = {}

--- Marks a table as a JSON array, so that an empty one is [] and not {}.
function M.array(t)
    return setmetatable(t, array)
end

--- JSON for a value made of strings, numbers, booleans and tables, keys in
--- order. KOReader has a JSON library, but this keeps the tests free of it.
function M.encode(value)
    local kind = type(value)
    if kind == "string" then
        return quote(value)
    elseif kind == "number" then
        if value == math.floor(value) and math.abs(value) < 1e15 then
            return ("%d"):format(value)
        end
        return ("%.10g"):format(value)
    elseif kind == "boolean" then
        return tostring(value)
    elseif kind == "table" then
        local parts = {}
        if getmetatable(value) == array or #value > 0 then
            for _i, v in ipairs(value) do
                table.insert(parts, M.encode(v))
            end
            return "[" .. table.concat(parts, ",") .. "]"
        end
        local keys = {}
        for k in pairs(value) do table.insert(keys, k) end
        table.sort(keys)
        for _i, k in ipairs(keys) do
            table.insert(parts, quote(k) .. ":" .. M.encode(value[k]))
        end
        return "{" .. table.concat(parts, ",") .. "}"
    end
    return "null"
end

local camel = {
    locator_end = "locatorEnd", created_at = "createdAt", updated_at = "updatedAt",
}

--- The body of the request for a sidecar's highlights: all of the device's,
--- which is how GOtome learns of those it no longer has. client is the
--- device's own name for itself.
function M.body(sidecar, client, page_size)
    local items = {}
    for _i, item in ipairs(M.collect(sidecar, page_size)) do
        local converted = {}
        for k, v in pairs(item) do converted[camel[k] or k] = v end
        if converted.rects then M.array(converted.rects) end
        table.insert(items, converted)
    end
    return M.encode({ clientId = client, annotations = M.array(items) })
end

return M
