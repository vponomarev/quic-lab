package ru.vpnc.quiclab

import android.content.Context
import android.util.AtomicFile
import java.io.File
import java.io.FileNotFoundException
import java.io.IOException
import org.json.JSONArray
import org.json.JSONObject

/**
 * Versioned portable model plus local preferences, projected from the legacy store.
 * Legacy preferences and encrypted identities remain intact during the transition.
 */
internal object VpnConfiguration {
    private const val FILE = "vpn-configuration.json"
    private val localKeys = listOf(
        "transport", "endpoint", "quic_endpoint", "https_endpoint", "awg_endpoint",
        "hostname", "dns", "mode", "routes", "apps", "global_apps", "ca",
        "server_name", "verify_name", "control_url", "data_version",
        "transit_endpoint", "max_availability", "bond_copy_budget", "bond_cell_budget",
        "available_transports",
    )

    @Synchronized fun load(context: Context): JSONObject = migrate(context)

    @Synchronized fun migrate(context: Context): JSONObject {
        val desired = legacySnapshot(context)
        mobile.Mobile.validateVpnConfiguration(desired.getJSONObject("model").toString())
        val bytes = desired.toString().toByteArray(Charsets.UTF_8)
        val file = AtomicFile(File(context.filesDir, FILE))
        val old = try { file.readFully() } catch (_: FileNotFoundException) { null }
        if (old != null) {
            val previous = JSONObject(old.toString(Charsets.UTF_8))
            require(previous.getInt("schema_version") == 1) { "Неподдерживаемая версия настроек VPN" }
            if (old.contentEquals(bytes)) return previous
        }
        val output = file.startWrite()
        try {
            output.write(bytes)
            file.finishWrite(output)
        } catch (failure: Exception) {
            file.failWrite(output)
            throw failure
        }
        if (!file.readFully().contentEquals(bytes)) throw IOException("Не удалось сохранить настройки VPN")
        return desired
    }

    private fun legacySnapshot(context: Context): JSONObject {
        val meta = context.getSharedPreferences("vpn_profiles", Context.MODE_PRIVATE)
        val list = JSONArray(meta.getString("profiles", null)
            ?: """[{"id":"default","name":"Основной"}]""")
        require(list.length() > 0) { "Нет профилей VPN" }
        val exits = JSONArray()
        val profiles = JSONArray()
        val localProfiles = JSONObject()
        val ids = mutableListOf<String>()
        for (i in 0 until list.length()) {
            val item = list.getJSONObject(i)
            val id = item.getString("id")
            require(id == "default" || id.matches(Regex("[a-f0-9-]{36}"))) { "Некорректный ID профиля" }
            ids.add(id)
            val prefs = context.getSharedPreferences(if (id == "default") "vpn" else "vpn_$id", Context.MODE_PRIVATE)
            val transport = prefs.getString("transport", "quic").orEmpty()
            val endpoint = prefs.getString("endpoint", "").orEmpty()
            val demux = transport == "quic" && prefs.getBoolean("max_availability", false)
            val exit = JSONObject().put("id", id).put("name", item.getString("name"))
                .put("kind", if (demux) "demux" else "standalone")
            if (demux) exit.put("demux_id", "legacy:$id")
            exits.put(exit)
            profiles.put(JSONObject().put("id", id).put("exit_id", id).put("transport", transport)
                .put("mode", if (endpoint.isBlank()) "disabled" else "auto")
                .put("endpoint", endpoint).put("priority", i).put("pool_size", 1)
                .put("check_reserve", false))
            val local = JSONObject()
            val all = prefs.all
            for (key in localKeys) {
                val value = all[key] ?: continue
                local.put(key, if (value is Set<*>) JSONArray(value.filterIsInstance<String>().sorted()) else value)
            }
            local.put("identity_ref", if (id == "default") "vpn-identity.enc" else "vpn-identity-$id.enc")
            localProfiles.put(id, local)
        }
        val enabled = meta.getStringSet("multiple_enabled", emptySet()).orEmpty()
        val dns = meta.getString("multiple_dns", "").orEmpty().ifBlank {
            ids.firstOrNull { it in enabled } ?: ids.first()
        }
        val local = JSONObject()
            .put("current_exit_id", meta.getString("current", ids.first()))
            .put("multiple", meta.getBoolean("multiple", false))
            .put("enabled_exit_ids", JSONArray(ids.filter { it in enabled }))
            .put("dns_exit_id", dns)
            .put("global_apps", JSONArray(meta.getStringSet("global_apps",
                context.getSharedPreferences("vpn",0).getStringSet("apps", emptySet())).orEmpty().sorted()))
            .put("profiles", localProfiles)
        val model = JSONObject().put("version", 1).put("exits", exits).put("profiles", profiles)
        return JSONObject().put("schema_version", 1).put("model", model).put("local", local)
    }
}
