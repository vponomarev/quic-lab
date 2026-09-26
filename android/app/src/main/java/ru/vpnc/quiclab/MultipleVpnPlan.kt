package ru.vpnc.quiclab

import android.content.Context
import org.json.JSONArray
import org.json.JSONObject

internal data class MultipleProfile(
    val id: String,
    val name: String,
    val endpoint: String,
    val hostname: String,
    val config: JSONObject,
)

internal data class MultipleVpnPlan(
    val profiles: List<MultipleProfile>,
    val rules: String,
    val dnsProfile: String,
    val dns: String,
) {
    companion object {
        fun load(c: Context): MultipleVpnPlan {
            val ps = VpnProfiles.list(c).filter { it.id in VpnProfiles.enabled(c) }
            require(ps.isNotEmpty()) { "Выберите профили multiple" }
            val rules = JSONArray()
            val profiles =
                ps.map { profile ->
                    val p = VpnProfiles.preferences(c, profile.id)
                    val mode = p.getInt("mode", 0)
                    val rule =
                        JSONObject()
                            .put("id", profile.id)
                            .put("mode", listOf("all", "apps", "exclude", "subnets")[mode])
                    if (mode == 1 || mode == 2) {
                        val uids =
                            VpnProfiles.apps(c, profile.id)
                                .filter { it != c.packageName }
                                .map {
                                    try {
                                        c.packageManager.getApplicationInfo(it, 0).uid.toLong()
                                    } catch (e: Exception) {
                                        throw IllegalArgumentException(
                                            "${profile.name}: приложение $it не установлено"
                                        )
                                    }
                                }
                                .distinct()
                        require(mode != 1 || uids.isNotEmpty()) {
                            "${profile.name}: выберите приложения"
                        }
                        rule.put("uids", JSONArray(uids))
                    }
                    if (mode == 3)
                        rule.put(
                            "subnets",
                            JSONArray(
                                VpnRoutes.parse(p.getString("routes", "")!!).map {
                                    "${it.first}/${it.second}"
                                }
                            ),
                        )
                    rules.put(rule)
                    val endpoint = p.getString("endpoint", "").orEmpty()
                    require(endpoint.isNotBlank()) { "${profile.name}: задайте сервер" }
                    val dns = p.getString("dns", "1.1.1.1").orEmpty()
                    VpnRoutes.parse("$dns/32")
                    val cfg =
                        VpnIdentity.load(c, profile.id)
                            .put("transport", p.getString("transport", "quic"))
                            .put("ca", p.getString("ca", ""))
                            .put("dns", dns)
                            .put("transit_endpoint", p.getString("transit_endpoint", ""))
                            .put("probe_exit_ip", false)
                    MultipleProfile(
                        profile.id,
                        profile.name,
                        endpoint,
                        p.getString("hostname", "").orEmpty(),
                        cfg,
                    )
                }
            val dnsProfile = VpnProfiles.dnsProfile(c).ifBlank { profiles.first().id }
            require(profiles.any { it.id == dnsProfile }) { "DNS-профиль должен быть включён" }
            return MultipleVpnPlan(
                profiles,
                rules.toString(),
                dnsProfile,
                VpnProfiles.preferences(c, dnsProfile).getString("dns", "1.1.1.1")!!,
            )
        }
    }
}
