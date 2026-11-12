-- What the plugin sends of KOReader's highlights, read from sidecars in the
-- shapes KOReader writes them (invented books): luajit
-- testdata/annotations_test.lua, from the package's folder (make
-- test-koplugin).
package.path = "gotome.koplugin/?.lua;" .. package.path
local annotations = require("gotome_annotations")
local feed = require("gotome_feed")

local failures = 0
local function check(name, got, want)
    if got ~= want then
        failures = failures + 1
        io.stderr:write(("FAIL %s: got %q, want %q\n"):format(name, tostring(got), tostring(want)))
    end
end

local function sidecar(path)
    local chunk = assert(loadfile(path))
    return chunk()
end

local new = sidecar("testdata/sidecar-new.lua")
local list = annotations.collect(new)
check("a bookmark is no highlight", #list, 2)
check("passage", list[1].passage, "It is a truth universally acknowledged")
check("kind", list[1].kind, "xpointer")
check("start", list[1].locator, "/body/DocFragment[3]/body/div/p[2]/text().4")
check("end", list[1].locator_end, "/body/DocFragment[3]/body/div/p[2]/text().40")
check("colour as KOReader names it", list[1].color, "olive")
check("chapter", list[1].chapter, "Chapter 1")
check("fraction from the page", list[1].fraction, 31 / 300)
check("key", list[1].key, "2026-03-01 10:00:00|/body/DocFragment[3]/body/div/p[2]/text().4|/body/DocFragment[3]/body/div/p[2]/text().40")
check("no note", list[1].note, nil)
check("note", list[2].note, 'Compare with "chapter one"\nand the \\ ending')
check("the key does not hold the note", list[2].key:find("chapter one", 1, true), nil)
check("document", annotations.document(new), "d41d8cd98f00b204e9800998ecf8427e")

local old = sidecar("testdata/sidecar-old.lua")
list = annotations.collect(old)
check("older: both highlights, in page order", #list, 2)
check("older: first", list[1].passage, "Chapter one begins")
check("older: second", list[2].passage, "The more I see of the world")
check("older: the note from the bookmark", list[2].note, "Remember this when choosing")
check("older: chapter is not known of a highlight alone", list[1].chapter, nil)
check("older: fraction", list[2].fraction, 28 / 200)
check("older: document", annotations.document(old), "0cc175b9c0f1b6a831c399e269772661")

local pdf = sidecar("testdata/sidecar-pdf.lua")
list = annotations.collect(pdf, function(page)
    check("asked for the page", page, 12)
    return { w = 600, h = 800 }
end)
check("pdf: one", #list, 1)
check("pdf: kind", list[1].kind, "pdf")
check("pdf: page", list[1].page, 12)
check("pdf: locator", list[1].locator, "page:12")
check("pdf: rectangles", #list[1].rects, 2)
check("pdf: x as a fraction of the page", list[1].rects[1].x, 0.1)
check("pdf: y", list[1].rects[1].y, 0.15)
check("pdf: w", list[1].rects[2].w, 0.25)
check("pdf: fraction", list[1].fraction, 0.1)
check("pdf without page sizes has no rectangles", annotations.collect(pdf)[1].rects, nil)

local body = annotations.body(pdf, "koreader:TOLINO1", function() return { w = 600, h = 800 } end)
check("body", body, '{"annotations":[{"chapter":"Part Two","color":"yellow","createdAt":"2026-04-01 12:00:00",'
    .. '"fraction":0.1,"key":"2026-04-01 12:00:00|page:12:60:120|page:12:210:144","kind":"pdf","locator":"page:12",'
    .. '"note":"Check this","page":12,"passage":"A passage over two lines",'
    .. '"rects":[{"h":0.015,"w":0.5,"x":0.1,"y":0.15},{"h":0.015,"w":0.25,"x":0.1,"y":0.165}]}],"clientId":"koreader:TOLINO1"}')
check("a book without highlights sends none, which removes them", annotations.body({}, "koreader:X"),
    '{"annotations":[],"clientId":"koreader:X"}')
check("quoting", annotations.encode("a\"b\\c\n\1"), '"a\\"b\\\\c\\n\\u0001"')

check("api address", feed.api_url("https://books.example.org/", "/device/annotations/ab"),
    "https://books.example.org/api/v1/device/annotations/ab")
check("api address from the catalogue's", feed.api_url("http://nas:25560/api/v1/opds", "/x"), "http://nas:25560/api/v1/x")

local chunk, err = loadfile("gotome.koplugin/gotome_annotations.lua")
check("compiles", chunk ~= nil, true)
if err then io.stderr:write(err .. "\n") end

if failures > 0 then
    os.exit(1)
end
print("ok: the plugin's highlights")
