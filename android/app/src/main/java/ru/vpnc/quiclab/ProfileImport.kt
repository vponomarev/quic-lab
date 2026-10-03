package ru.vpnc.quiclab
import android.content.Context
import org.json.JSONObject
import java.net.URI
import javax.net.ssl.HttpsURLConnection

internal object ProfileImport {
    fun reviewAddress(p:JSONObject):String = transports(p).joinToString("\n") { protocol ->
        when(protocol) {
            "awg" -> "AmneziaWG: ${AwgImport.metadata(p.getString("awg_config")).getString("endpoint")}"
            "vless" -> "VLESS: ${VlessImport.metadata(p.getString("vless_uri")).getString("endpoint")}"
            else -> "${protocol.uppercase()}: ${p.getString(protocol)}"
        }
    }

    const val REQUEST = 4020
    fun transports(p: JSONObject): List<String> {
        if (p.optInt("version") == 1) return listOf("quic", "https")
        val array = p.getJSONArray("transports")
        val items = (0 until array.length()).map { array.getString(it) }
        require(items.isNotEmpty() && items.size <= 4 && items.distinct() == items && items.all { it in listOf("quic", "https", "awg", "vless") }) { "Некорректный список протоколов" }
        return items
    }
    /** Managed profiles carry exactly one URI; arbitrary outbound JSON stays in manual import. */
    fun vlessConfig(p: JSONObject): String? {
        if (!p.has("vless_uri")) return null
        val raw = p.getString("vless_uri")
        require(raw.startsWith("vless://") && !raw.contains('\n') && !raw.contains('\r')) { "Нужен один VLESS URI" }
        val canonical = mobile.Mobile.importVLESSConfig(raw)
        mobile.Mobile.validateVLESSConfig(raw)
        return canonical
    }
    fun validate(p: JSONObject): String {
        require(p.getInt("version") in listOf(1, 2)) { "Неподдерживаемая версия профиля" }
        val kind=p.getString("kind")
        if(kind=="capture") { mobile.Mobile.validateDebugCapture(p.toString()); return kind }
        require(kind == "echo" || kind == "vpn") { "Неизвестный профиль" }
        require(p.getString("hostname").matches(Regex("[a-zA-Z0-9.-]{1,253}"))) { "Некорректное TLS имя" }
        if (kind == "vpn") {
        val serverName = p.optString("server_name")
        val verifyName = p.optString("verify_name")
        require(serverName.isBlank() || serverName.matches(Regex("[a-zA-Z0-9.-]{1,253}"))) { "Некорректное server_name" }
        require(verifyName.isBlank() || verifyName.matches(Regex("[a-zA-Z0-9.-]{1,253}"))) { "Некорректное verify_name" }
        require(serverName.isBlank() || verifyName.isNotBlank()) { "Для server_name требуется verify_name" }
        if (p.optString("control_url").isNotBlank()) {
            val control = p.getString("control_url")
            val u = URI(control)
            require(u.scheme == "https" && u.host != null && (u.port == -1 || u.port in 1..65535) && u.userInfo == null && u.query == null && u.fragment == null) { "Некорректный control_url" }
        }
        }
        fun endpoint(key:String) {
            val value=p.getString(key); val pieces=value.split(":")
            require(pieces.size==2 && pieces[0].matches(Regex("[a-zA-Z0-9.-]{1,253}")) && (pieces[1].toIntOrNull() ?: 0) in 1..65535) { "Неверный адрес $key" }
        }
        if(kind=="echo") { endpoint("endpoint"); if(p.optString("https").isNotBlank()) endpoint("https");require(!p.has("key") && !p.has("certificate")) { "Echo не должен содержать ключи" }
            require(p.optString("pin").isEmpty() || p.optString("pin").matches(Regex("[0-9a-fA-F]{64}"))) { "Некорректный fingerprint" }
        } else {
            ServiceTransfer.validateMetadata(p)
            val allowed = transports(p)
            require(p.has("vless_uri") == ("vless" in allowed)) { "VLESS URI должен соответствовать списку протоколов" }
            if ("vless" in allowed) vlessConfig(p)
            if (p.optString("transit_endpoint").isNotBlank()) endpoint("transit_endpoint")
            if ("quic" in allowed) endpoint("quic")
            if ("https" in allowed) endpoint("https")
            if ("awg" in allowed) mobile.Mobile.validateAWGConfig(p.getString("awg_config"))
            require(p.optInt("mode",0) in listOf(0,3)) { "Неподдерживаемый режим" }
            if(p.optInt("mode")==3) VpnRoutes.parse(p.getString("routes"))
            VpnRoutes.parse(p.optString("dns","1.1.1.1")+"/32")
            if (allowed.any { it in listOf("quic", "https") }) require(p.getString("certificate").length<16000 && p.getString("key").length<8000) { "Слишком большой сертификат" }
        }
        return kind
    }
    fun enrollment(raw:String):URI {
        val u=URI(raw)
        val capture=u.path?.endsWith("/capture/enroll")==true
        val tokenLength=if(capture) 48 else 64
        require(u.scheme=="https" && u.host!=null && (u.port==-1 || u.port in 1..65535) && u.userInfo==null && u.query==null && u.path?.endsWith("/enroll")==true && u.fragment?.matches(Regex("[0-9a-f]{$tokenLength}"))==true) { "Нужен QR регистрации QUIC Lab" }
        return u
    }
    fun enrollmentEndpoint(raw:String):URI {
        val u=enrollment(raw)
        return URI(u.scheme,null,u.host,u.port,u.path,null,null)
    }
    private fun enrollmentKey(raw:String):String = java.security.MessageDigest.getInstance("SHA-256")
        .digest(raw.toByteArray(Charsets.UTF_8)).joinToString("") { "%02x".format(it.toInt() and 255) }
    private fun deviceName():String = (android.os.Build.MANUFACTURER+" "+android.os.Build.MODEL)
        .replace(Regex("[^a-zA-Z0-9 ._-]"),"_").take(100).ifBlank { "Android" }
    @Synchronized fun enrollmentRequest(context:Context,raw:String):JSONObject {
        val u=enrollment(raw)
        if(u.path.endsWith("/capture/enroll")) return JSONObject().put("token",u.fragment)
        val preferences=context.getSharedPreferences("enrollment_requests",Context.MODE_PRIVATE)
        val key=enrollmentKey(raw)
        val requestID=preferences.getString(key,null) ?: java.util.UUID.randomUUID().toString().also {
            check(preferences.edit().putString(key,it).commit()) { "Не удалось сохранить запрос регистрации" }
        }
        return JSONObject().put("token",u.fragment).put("request_id",requestID).put("device_name",deviceName())
    }
    @Synchronized fun completeEnrollment(context:Context,raw:String) {
        check(context.getSharedPreferences("enrollment_requests",Context.MODE_PRIVATE).edit().remove(enrollmentKey(raw)).commit()) { "Не удалось завершить регистрацию" }
    }
    fun fetch(context:Context,raw:String):JSONObject = fetchRequest(raw,enrollmentRequest(context,raw))
    // Capture imports retain their existing one-use contract. User-facing VPN
    // imports use the Context overload so response-loss retries survive restart.
    fun fetch(raw:String):JSONObject {
        val u=enrollment(raw)
        val body=JSONObject().put("token",u.fragment)
        require(u.path.endsWith("/capture/enroll")) { "Для регистрации VPN требуется сохранённый запрос импорта" }
        return fetchRequest(raw,body)
    }
    private fun fetchRequest(raw:String,body:JSONObject):JSONObject {
        val u=enrollment(raw)
        val c=enrollmentEndpoint(raw).toURL().openConnection() as HttpsURLConnection
        try {c.instanceFollowRedirects=false;c.connectTimeout=15000;c.readTimeout=15000;c.requestMethod="POST";c.doOutput=true;c.setRequestProperty("Content-Type","application/json")
            c.outputStream.use{it.write(body.toString().toByteArray(Charsets.UTF_8))}
            require(c.responseCode==200) { if(c.responseCode==410) "QR истёк, отозван или достиг лимита устройств. Получите новый QR." else "Сервер не выдал профиль (${c.responseCode})" }
            val out=java.io.ByteArrayOutputStream();c.inputStream.use { input -> val buf=ByteArray(4096);while(true){val n=input.read(buf);if(n<0)break;require(out.size()+n<=32768){"Профиль слишком большой"};out.write(buf,0,n)} }
            return JSONObject(out.toString("UTF-8")).also{require(validate(it)==(if(u.path.endsWith("/capture/enroll")) "capture" else "vpn")) { "Неожиданный тип профиля" }}
        } finally { c.disconnect() }
    }
    fun save(context:Context,p:JSONObject):String {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val kind=validate(p)
        if(kind=="capture") { mobile.Mobile.configureDebugCapture(p.toString()); return kind }
        if(kind=="echo") { check(context.getSharedPreferences("server",Context.MODE_PRIVATE).edit()
            .putString("endpoint",p.getString("endpoint")).putString("hostname",p.getString("hostname"))
            .putString("https_endpoint",p.optString("https")).putString("pin",p.optString("pin")).putString("awg_enroll_url",p.optString("awg_enroll_url")).putBoolean("compare",true).commit()) }
        else {
            val previous=VpnProfiles.current(context).id
            val added=VpnProfiles.create(context,(p.optString("name","VPN")+" · "+p.getString("hostname")).take(100))
            try {
            val allowed = transports(p)
            VpnIdentity.importBundle(context,p)
            val awg = if ("awg" in allowed) AwgImport.metadata(p.getString("awg_config")) else null
            val vless = if ("vless" in allowed) VlessImport.metadata(p.getString("vless_uri")) else null
            val selected = allowed.first()
            val address = when (selected) { "awg" -> awg!!.getString("endpoint"); "vless" -> vless!!.getString("endpoint"); else -> p.getString(selected) }
            check(VpnProfiles.preferences(context).edit()
                .putBoolean("managed_profile",p.optString("config_url").isNotEmpty())
                .putString("transit_endpoint",p.optString("transit_endpoint"))
                .putString("transport",selected).putString("endpoint",address)
                .putStringSet("available_transports",allowed.toSet())
                .putString("awg_endpoint",awg?.optString("endpoint") ?: "")
                .putString("vless_endpoint",vless?.optString("endpoint") ?: "")
                .putString("quic_endpoint",p.optString("quic")).putString("https_endpoint",p.optString("https"))
                .putString("hostname",p.getString("hostname")).putString("ca",p.optString("ca"))
                .putString("server_name",p.optString("server_name"))
                .putString("verify_name",p.optString("verify_name"))
                .putString("control_url",p.optString("control_url"))
                .putInt("data_version",p.optInt("data_version",0))
                .putString("dns",p.optString("dns","1.1.1.1")).putInt("mode",p.optInt("mode",0))
                .putString("routes",p.optString("routes")).putStringSet("apps",emptySet()).commit())
            VpnProfiles.configuration(context)
            } catch(e:Exception) {
                VpnProfiles.delete(context,added.id); VpnProfiles.select(context,previous); throw e
            }
        }
        return kind
    }
}
