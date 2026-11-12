-- rex config.lua: settings and themes in Lua (rex config edit -lua).
--
-- Values set here win over config.yaml and are never written to it; the
-- settings page shows them locked. rex config list shows where each value
-- comes from. Docs: docs/howto/lua-config.md
--
-- The file runs in a restricted Lua state: no os, io, print or load, and a
-- 250 ms limit. It must be owned by you and not writable by others.
--
-- rex.set(id, value)      -- any id from `rex config list`
-- rex.theme(name, table)  -- the theme file schema (docs/themes.md)
-- rex.platform            -- "darwin", "linux" or "windows"
-- require("name")         -- loads ~/.config/rex/lua/name.lua

-- rex.set("spinner", "moon")
-- rex.set("reduce_motion", true)

-- if rex.platform == "darwin" then
--   rex.set("screensaver_keep_awake", true)
-- end

-- A list setting takes a Lua list:
-- rex.set("auto_trust_dirs", { "~/dev", "~/work" })

-- rex.theme("mine", {
--   extends = "noir",
--   colors = { accent = "#B392F0" },
-- })
-- rex.set("theme", "mine")
