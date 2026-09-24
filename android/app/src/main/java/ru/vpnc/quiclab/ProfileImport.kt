package ru.vpnc.quiclab
import android.content.Context
import org.json.JSONObject
import java.net.URI
import javax.net.ssl.HttpsURLConnection

internal object ProfileImport {
    const val REQUEST = 4020
    fun validate(p: JSONObject): String {
        require(p.getInt("version") == 1) { "Неподдерживаемая версия профиля" }
        val kind=p.getString("kind")
        require(kind == "echo" || kind == "vpn") { "Неизвестный профиль" }
        require(p.getString("hostname").matches(Regex("[a-zA-Z0-9.-]{1,253}"))) { "Некорректное TLS имя" }
        fun endpoint(key:String) {
            val value=p.getString(key); val pieces=value.split(":")
            require(pieces.size==2 && pieces[0].matches(Regex("[a-zA-Z0-9.-]{1,253}")) && (pieces[1].toIntOrNull() ?: 0) in 1..65535) { "Неверный адрес $key" }
        }
        if(kind=="echo") { endpoint("endpoint");require(!p.has("key") && !p.has("certificate")) { "Echo не должен содержать ключи" }
            require(p.optString("pin").isEmpty() || p.optString("pin").matches(Regex("[0-9a-fA-F]{64}"))) { "Некорректный fingerprint" }
        } else {endpoint("quic");endpoint("https");require(p.optInt("mode",0) in listOf(0,3)) { "Неподдерживаемый режим" }
            if(p.optInt("mode")==3) VpnRoutes.parse(p.getString("routes"))
            VpnRoutes.parse(p.optString("dns","1.1.1.1")+"/32")
            require(p.getString("certificate").length<16000 && p.getString("key").length<8000) { "Слишком большой сертификат" }
        }
        return kind
    }
    fun enrollment(raw:String):URI {
        val u=URI(raw)
        require(u.scheme=="https" && u.host!=null && u.userInfo==null && u.query==null && u.path.endsWith("/enroll") && u.fragment?.matches(Regex("[0-9a-f]{48}"))==true) { "Нужен QR профиля QUIC Lab" }
        return u
    }
    fun fetch(raw:String):JSONObject {
        val u=enrollment(raw)
        val endpoint=URI(u.scheme,null,u.host,u.port,u.path,null,null).toURL()
        val c=endpoint.openConnection() as HttpsURLConnection
        try {c.instanceFollowRedirects=false;c.connectTimeout=15000;c.readTimeout=15000;c.requestMethod="POST";c.doOutput=true;c.setRequestProperty("Content-Type","application/json")
            c.outputStream.use{it.write(JSONObject().put("token",u.fragment).toString().toByteArray())}
            require(c.responseCode==200) { if(c.responseCode==410) "QR истёк или уже использован. Получите новый QR." else "Сервер не выдал профиль (${c.responseCode})" }
            val out=java.io.ByteArrayOutputStream();c.inputStream.use { input -> val buf=ByteArray(4096);while(true){val n=input.read(buf);if(n<0)break;require(out.size()+n<=32768){"Профиль слишком большой"};out.write(buf,0,n)} }
            return JSONObject(out.toString("UTF-8")).also{require(validate(it)=="vpn") { "Нужен VPN профиль" }}
        } finally { c.disconnect() }
    }
    fun save(context:Context,p:JSONObject):String {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val kind=validate(p)
        if(kind=="echo") { check(context.getSharedPreferences("server",Context.MODE_PRIVATE).edit()
            .putString("endpoint",p.getString("endpoint")).putString("hostname",p.getString("hostname"))
            .putString("pin",p.optString("pin")).putBoolean("compare",true).commit()) }
        else {
            val previous=VpnProfiles.current(context).id
            val added=VpnProfiles.create(context,(p.optString("name","VPN")+" · "+p.getString("hostname")).take(100))
            try { VpnIdentity.importProfile(context,p)
            check(VpnProfiles.preferences(context).edit()
                .putString("transport","quic").putString("endpoint",p.getString("quic"))
                .putString("quic_endpoint",p.getString("quic")).putString("https_endpoint",p.getString("https"))
                .putString("hostname",p.getString("hostname")).putString("ca",p.optString("ca"))
                .putString("dns",p.optString("dns","1.1.1.1")).putInt("mode",p.optInt("mode",0))
                .putString("routes",p.optString("routes")).putStringSet("apps",emptySet()).commit())
            } catch(e:Exception) {
                VpnProfiles.delete(context,added.id); VpnProfiles.select(context,previous); throw e
            }
        }
        return kind
    }
}
