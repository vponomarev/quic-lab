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
        "transport", "endpoint", "quic_endpoint", "https_endpoint", "awg_endpoint", "vless_endpoint",
        "hostname", "dns", "mode", "routes", "apps", "global_apps", "ca",
        "server_name", "verify_name", "control_url", "data_version",
        "transit_endpoint", "max_availability", "bond_copy_budget", "bond_cell_budget",
        "available_transports", "demux_enabled", "quic_mode", "https_mode", "quic_pool_size", "https_pool_size", "quic_check_reserve", "https_check_reserve",
    )

    /** One immutable view of the atomically committed server bundle. Editors
     * still write only local preferences; managed server fields are shown readonly. */
    fun effectivePreferences(context: Context, id: String): android.content.SharedPreferences {
        require(id == "default" || id.matches(Regex("[a-f0-9-]{36}")))
        val local = context.getSharedPreferences(if (id == "default") "vpn" else "vpn_$id", Context.MODE_PRIVATE)
        val identityFile=VpnProfiles.identityFile(context,id)
        if (!local.getBoolean("managed_profile",false)) return local
        val bundle=VpnIdentity.load(context,id)
        val server=bundle.optJSONObject("server_config") ?: return local
        val values=mutableMapOf<String,Any?>()
        val allowed=ProfileImport.transports(server)
        val selected=local.getString("transport","quic").orEmpty().let {if(it in allowed) it else allowed.first()}
        values["transport"]=selected;values["available_transports"]=allowed.toSet()
        for(k in listOf("hostname","dns","ca","server_name","verify_name","control_url","transit_endpoint")) values[k]=server.optString(k,if(k=="dns") "1.1.1.1" else "")
        values["data_version"]=server.optInt("data_version",1)
        values["quic_endpoint"]=server.optString("quic")
        values["https_endpoint"]=server.optString("https")
        val awg=if("awg" in allowed) AwgImport.metadata(server.getString("awg_config")) else null
        values["awg_endpoint"]=awg?.optString("endpoint") ?: ""
        val vless=if("vless" in allowed) VlessImport.metadata(server.getString("vless_uri")) else null
        values["vless_endpoint"]=vless?.optString("endpoint") ?: ""
        values["endpoint"]=when(selected) {"awg"->values["awg_endpoint"];"vless"->values["vless_endpoint"];"https"->values["https_endpoint"];else->values["quic_endpoint"]}
        // V2 VLESS is standalone. Keep the user's demux preference for a later QUIC/HTTPS selection.
        if(selected=="vless") values["demux_enabled"]=false
        return object:android.content.SharedPreferences by local {
            override fun getAll():MutableMap<String,*> = (local.all.toMutableMap().apply {putAll(values)})
            override fun contains(key:String)=key in values || local.contains(key)
            override fun getString(key:String,defValue:String?):String?=if(key in values) values[key] as? String else local.getString(key,defValue)
            override fun getInt(key:String,defValue:Int)=if(key in values) values[key] as? Int ?: defValue else local.getInt(key,defValue)
            override fun getBoolean(key:String,defValue:Boolean)=if(key in values) values[key] as? Boolean ?: defValue else local.getBoolean(key,defValue)
            @Suppress("UNCHECKED_CAST") override fun getStringSet(key:String,defValues:MutableSet<String>?):MutableSet<String>? =
                if(key in values) (values[key] as? Set<String>)?.toMutableSet() else local.getStringSet(key,defValues)
        }
    }
    /** Credentials enter only this in-memory runtime envelope, never the portable snapshot. */
    fun attachRuntime(context:Context,id:String,config:JSONObject):JSONObject {
        val model=load(context).getJSONObject("model")
        val exits=model.getJSONArray("exits")
        val exit=(0 until exits.length()).map{exits.getJSONObject(it)}.single{it.getString("id")==id}
        if(exit.getString("kind")!="demux") {config.put("max_availability",false);return config}
        val p=effectivePreferences(context,id)
        val profiles=JSONArray();val gateways=JSONObject();val all=model.getJSONArray("profiles")
        for(i in 0 until all.length()) {
            val profile=all.getJSONObject(i);if(profile.getString("exit_id")!=id)continue
            profiles.put(JSONObject(profile.toString()))
            gateways.put(profile.getString("id"),JSONObject(config.toString())
                .put("transport",profile.getString("transport")).put("endpoint",profile.getString("endpoint"))
                .put("hostname",p.getString("hostname","")).put("max_availability",true))
        }
        return config.put("_exit_runtime",JSONObject()
            .put("model",JSONObject().put("version",1).put("exits",JSONArray().put(JSONObject(exit.toString()))).put("profiles",profiles))
            .put("gateways",gateways).put("economy",!p.getBoolean("max_availability",false)))
    }
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
            val prefs = effectivePreferences(context,id)
            val transport = prefs.getString("transport", "quic").orEmpty()
            val endpoint = prefs.getString("endpoint", "").orEmpty()
            val demux = transport in listOf("quic","https") && prefs.getBoolean("demux_enabled",prefs.getBoolean("max_availability",false))
            val exit = JSONObject().put("id", id).put("name", item.getString("name"))
                .put("kind", if (demux) "demux" else "standalone")
            if (demux) exit.put("demux_id", "legacy:$id")
            exits.put(exit)
            val available=prefs.getStringSet("available_transports",null)
            val transports=if(demux) listOf(transport)+listOf("quic","https").filter {
                it!=transport && !prefs.getString("${it}_endpoint","").isNullOrBlank() && (available==null || it in available)
            } else listOf(transport)
            transports.forEachIndexed { priority, kind ->
                val address=if(kind==transport) endpoint else prefs.getString("${kind}_endpoint","").orEmpty()
                val profileId=if(kind==transport) id else "$id.$kind"
                val profileMode=if(address.isBlank()) "disabled" else if(demux) prefs.getString("${kind}_mode","auto") else "auto"
                val defaultPool=if(demux && kind==transport && prefs.getBoolean("max_availability",false)) 2 else 1
                profiles.put(JSONObject().put("id",profileId).put("exit_id",id).put("transport",kind)
                    .put("mode",profileMode).put("endpoint",address).put("priority",priority)
                    .put("pool_size",if(demux) prefs.getInt("${kind}_pool_size",defaultPool) else 1)
                    .put("check_reserve",demux && prefs.getBoolean("${kind}_check_reserve",false)))
            }
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
