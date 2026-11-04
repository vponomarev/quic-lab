package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONObject
import java.io.File
import java.net.URI

/** Server egress is independent of the local profile/group exit ID. */
internal object ServerExitSelection {
    data class Choice(val id: String, val name: String, val disabled: Boolean)
    data class View(val selected: String, val effective: String, val legacy: Boolean,
                    val blocked: Boolean, val pending: Boolean, val revision: String, val choices: List<Choice>)
    private val idPattern = Regex("[a-z][a-z0-9_-]{0,63}")
    private val hexPattern = Regex("[a-fA-F0-9]{64}")

    fun endpoint(identity: JSONObject): String {
        val id = identity.optString("device_id")
        val raw = identity.optString("config_url")
        require(id.matches(Regex("[a-zA-Z0-9_-]{1,128}")) &&
            identity.optString("update_token").matches(hexPattern)) { "Нужен профиль с ключом управления устройством" }
        ServiceTransfer.validateMetadata(identity)
        val u = URI(raw)
        require(raw.length <= 2048 && u.rawPath == "/api/v1/devices/$id/config") { "Адрес управления не соответствует устройству" }
        return raw.removeSuffix("/config") + "/server-exit"
    }

    fun parse(raw: String): View {
        require(raw.toByteArray().size <= 65536)
        val o = JSONObject(raw)
        val revision = o.getString("revision")
        val selected = o.getString("selected")
        require(revision.matches(hexPattern) && (selected.isEmpty() || selected.matches(idPattern)))
        val a = o.getJSONArray("choices")
        require(a.length() <= 32)
        val choices = (0 until a.length()).map { i ->
            val c = a.getJSONObject(i)
            val id = c.getString("id"); val name = c.getString("name")
            require(id.matches(idPattern) && c.getString("protocol") == "awg" &&
                name.isNotEmpty() && name.toByteArray().size <= 128 && name.none { Character.isISOControl(it) })
            Choice(id, name, c.optBoolean("disabled"))
        }
        require(choices.map { it.id }.distinct().size == choices.size)
        val v = View(selected, o.getString("effective"), o.getBoolean("legacy"),
            o.getBoolean("blocked"), o.getBoolean("pending"), revision, choices)
        require(v.effective.isEmpty() || choices.any { it.id == v.effective })
        require(v.legacy || v.blocked || v.effective.isNotEmpty())
        return v
    }

    fun fetch(context: Context, profile: String, consent: Boolean = false): View =
        request(context, profile, null, consent)

    fun select(context: Context, profile: String, id: String, revision: String, consent: Boolean = false): View {
        require((id.isEmpty() || id.matches(idPattern)) && revision.matches(hexPattern))
        val view = request(context, profile, JSONObject().put("server_exit_id", id).put("revision", revision).toString(), consent)
        // The server closes old flows before acknowledging the change.
        LabVpnService.restartUpdatedExit(context, profile)
        return view
    }

    private fun request(context: Context, profile: String, body: String?, consent: Boolean): View {
        endpoint(VpnIdentity.load(context, profile))
        val file = File.createTempFile("server-exit-", ".json", context.cacheDir)
        try {
            LabVpnService.fetchUpdate(context, profile, if (body == null) "server-exits" else "select-server-exit",
                file, consent, requestBody = body)
            return parse(file.readText())
        } finally { file.delete() }
    }
}
