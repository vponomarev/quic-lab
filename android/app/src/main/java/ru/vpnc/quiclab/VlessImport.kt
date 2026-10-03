package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import android.text.InputType
import android.view.View
import android.widget.EditText
import mobile.Mobile
import org.json.JSONObject

/** Manual fixed import only. Credentials remain in the encrypted identity bundle. */
internal object VlessImport {
    fun metadata(raw: String) = JSONObject(Mobile.validateVLESSConfig(raw))

    fun save(context: Context, raw: String) {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val info = metadata(raw)
        val endpoint = info.getString("endpoint")
        val name = info.optString("name").ifBlank { endpoint }
        val previous = VpnProfiles.current(context).id
        val profile = VpnProfiles.create(context, "VLESS · $name".take(100))
        try {
            VpnIdentity.importVLESS(context, raw)
            check(VpnProfiles.preferences(context).edit()
                .putString("transport", "vless")
                .putStringSet("available_transports", setOf("vless"))
                .putString("endpoint", endpoint)
                .putString("vless_endpoint", endpoint)
                .putString("dns", "1.1.1.1")
                .putString("routes", "0.0.0.0/0")
                .putInt("mode", 0)
                .putBoolean("demux_enabled", false)
                .commit())
        } catch (failure: Exception) {
            try { VpnProfiles.delete(context, profile.id) }
            finally { VpnProfiles.select(context, previous) }
            throw failure
        }
    }

    private fun error(activity: Activity) {
        AlertDialog.Builder(activity).setTitle("Импорт VLESS не выполнен")
            .setMessage("Проверьте URI или один VLESS outbound JSON: TCP, TLS/REALITY, корректные параметры. VPN должен быть остановлен.")
            .setPositiveButton("OK", null).show()
    }

    fun review(activity: Activity, raw: String, saved: () -> Unit, cancelled: () -> Unit = {}) {
        val info = metadata(raw)
        val name = info.optString("name").ifBlank { "VLESS" }
        val security = if (info.getString("security") == "reality") "REALITY" else "TLS"
        val flow = if (info.optString("flow").isNotBlank()) " · Vision" else ""
        AlertDialog.Builder(activity).setTitle("Добавить профиль VLESS?")
            .setMessage("$name\nСервер: ${info.getString("endpoint")}\nЗащита: $security$flow\nDNS: 1.1.1.1\nIPv4 через отдельный VPN-профиль. При смене сети соединения могут прерваться.\nДанные доступа будут сохранены в зашифрованном хранилище.")
            .setNegativeButton("Отмена") { _, _ -> cancelled() }
            .setOnCancelListener { cancelled() }
            .setPositiveButton("Импортировать") { _, _ ->
                try { save(activity, raw); saved() }
                catch (_: Exception) { error(activity) }
            }.show()
    }

    fun prompt(activity: Activity, saved: () -> Unit) {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val input = EditText(activity).apply {
            hint = "vless://… или один outbound JSON"
            inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_FLAG_MULTI_LINE or InputType.TYPE_TEXT_FLAG_NO_SUGGESTIONS
            importantForAutofill = View.IMPORTANT_FOR_AUTOFILL_NO
            isSaveEnabled = false
            minLines = 3
            maxLines = 8
        }
        AlertDialog.Builder(activity).setTitle("Импорт VLESS URI / JSON").setView(input)
            .setNegativeButton("Отмена") { _, _ -> input.text.clear() }
            .setOnCancelListener { input.text.clear() }
            .setPositiveButton("Проверить") { _, _ ->
                val raw = input.text.toString()
                input.text.clear()
                try { review(activity, raw, saved) }
                catch (_: Exception) { error(activity) }
            }.show()
    }
}
