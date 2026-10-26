-- NowPlaying for OBS: starts NowPlaying.exe together with OBS.
-- NowPlaying closes itself a few seconds after OBS closes, so nothing is
-- detected while you're not streaming.
--
-- Keep this file in the same folder as NowPlaying.exe, then in OBS:
-- Tools -> Scripts -> + -> pick this file.

obs = obslua

function script_description()
	return "<b>NowPlaying</b><br>Starts NowPlaying together with OBS (hidden in the tray) " ..
	       "and it closes by itself when OBS closes.<br><br>" ..
	       "Keep this script in the same folder as NowPlaying.exe."
end

-- Reload every browser source that shows the NowPlaying overlay, so it
-- connects even if OBS loaded it before the app was ready.
local function refresh_overlays()
	obs.timer_remove(refresh_overlays)
	local sources = obs.obs_enum_sources()
	if sources ~= nil then
		for _, source in ipairs(sources) do
			if obs.obs_source_get_unversioned_id(source) == "browser_source" then
				local s = obs.obs_source_get_settings(source)
				local url = obs.obs_data_get_string(s, "url") .. " " .. obs.obs_data_get_string(s, "local_file")
				obs.obs_data_release(s)
				if url:find("8787", 1, true) or url:find("OBS%-overlay") then
					local props = obs.obs_source_properties(source)
					local btn = obs.obs_properties_get(props, "refreshnocache")
					if btn ~= nil then obs.obs_property_button_clicked(btn, source) end
					obs.obs_properties_destroy(props)
				end
			end
		end
		obs.source_list_release(sources)
	end
end

function script_load(settings)
	local exe = (script_path() .. "NowPlaying.exe"):gsub("/", "\\")
	local f = io.open(exe, "rb")
	if f == nil then
		obs.script_log(obs.LOG_WARNING, "NowPlaying.exe not found next to this script: " .. exe)
		return
	end
	f:close()
	-- -tray: start quietly in the tray; -with-obs: quit when OBS closes
	os.execute('start "" "' .. exe .. '" -tray -with-obs')
	obs.script_log(obs.LOG_INFO, "Started " .. exe)
	obs.timer_add(refresh_overlays, 4000)
end
