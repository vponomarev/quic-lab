package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Context
import mobile.Mobile
import org.json.JSONObject

internal object AwgImport {
    fun metadata(raw: String) = JSONObject(Mobile.validateAWGConfig(raw))
    fun save(context: Context, raw: String) {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val info = metadata(raw)
        val previous = VpnProfiles.current(context).id
        val profile = VpnProfiles.create(context, "AWG · ${info.getString("endpoint")}".take(100))
        try {
            VpnIdentity.importAWG(context, raw)
            val allowed = info.getJSONArray("allowed_ips")
            val routes = (0 until allowed.length()).map { allowed.getString(it) }
            check(VpnProfiles.preferences(context).edit()
                .putString("transport", "awg")
                .putStringSet("available_transports", setOf("awg"))
                .putString("endpoint", info.getString("endpoint"))
                .putString("awg_endpoint", info.getString("endpoint"))
                .putString("dns", info.getString("dns"))
                .putString("routes", routes.joinToString("\n"))
                .putInt("mode", if ("0.0.0.0/0" in routes) 0 else 3)
                .commit())
        } catch (e: Exception) {
            VpnProfiles.delete(context, profile.id)
            VpnProfiles.select(context, previous)
            throw e
        }
    }
    fun review(activity: Activity, raw: String, saved: () -> Unit, cancelled: () -> Unit = {}) {
        val info = metadata(raw)
        val warning = if (info.optBoolean("ipv6_ignored")) "\nIPv6 не поддерживается и будет заблокирован для трафика, захваченного VPN." else ""
        AlertDialog.Builder(activity).setTitle("Добавить профиль AmneziaWG?")
            .setMessage("Сервер: ${info.getString("endpoint")}\nIPv4: ${info.getString("address")}\nDNS: ${info.getString("dns")}\nКлючи будут сохранены в зашифрованном хранилище.$warning")
            .setNegativeButton("Отмена") { _, _ -> cancelled() }
            .setPositiveButton("Импортировать") { _, _ ->
                try { save(activity, raw); saved() }
                catch (e: Exception) { AlertDialog.Builder(activity).setTitle("Импорт не выполнен").setMessage(e.message).setPositiveButton("OK", null).show() }
            }.show()
    }
}
