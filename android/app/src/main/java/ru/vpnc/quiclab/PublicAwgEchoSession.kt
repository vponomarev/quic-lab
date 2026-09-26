package ru.vpnc.quiclab

import android.content.Context
import android.net.ConnectivityManager
import java.net.URL
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import javax.net.ssl.HttpsURLConnection
import org.json.JSONObject

/** Short-lived echo-only credentials remain in memory and never become a VPN profile. */
internal class PublicAwgEchoSession(
    private val context: Context,
    private val url: String,
    private val output: (JSONObject) -> Unit,
) {
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private var session: VpnSession? = null
    private var endpoint = ""
    private var requested = VpnSession.WIFI
    private var started = false
    @Volatile private var closed = false
    @Volatile private var connection: HttpsURLConnection? = null

    @Synchronized
    fun move(kind: Int) {
        if (closed) return
        requested = kind
        if (session != null) {
            session?.startOrMigrate(kind, endpoint, "", "", 1000)
            return
        }
        if (started) return
        started = true
        worker.execute {
            try {
                val u = URL(url)
                require(u.protocol == "https" && u.userInfo == null) {
                    "Echo enrollment requires HTTPS"
                }
                val cm = context.getSystemService(ConnectivityManager::class.java)
                val network =
                    cm.allNetworks.firstOrNull {
                        cm.getNetworkCapabilities(it)?.hasTransport(kind) == true
                    } ?: error("Сеть недоступна")
                val http = network.openConnection(u) as HttpsURLConnection
                connection = http
                val cfg =
                    try {
                        http.requestMethod = "POST"
                        http.connectTimeout = 8000
                        http.readTimeout = 8000
                        http.instanceFollowRedirects = false
                        require(http.responseCode == 200) {
                            "AWG Echo недоступен (${http.responseCode})"
                        }
                        val raw = http.inputStream.use { it.readBytesLimited(16384) }
                        JSONObject(String(raw, Charsets.UTF_8))
                    } finally {
                        http.disconnect()
                        connection = null
                    }
                val expires = cfg.getLong("expires_at") * 1000 - System.currentTimeMillis()
                require(expires in 1000..660000) { "Invalid Echo lifetime" }
                require(cfg.optString("transport") == "awg")
                val info = AwgImport.metadata(cfg.getString("awg_config"))
                endpoint = info.getString("endpoint")
                synchronized(this) {
                    if (closed) return@execute
                    session =
                        VpnSession(
                            cm,
                            null,
                            cfg,
                            -1,
                            availability = { _, availableKind, present ->
                                synchronized(this) {
                                    if (!closed && present && availableKind == requested)
                                        session?.startOrMigrate(requested, endpoint, "", "", 1000)
                                }
                            },
                            output = { if (!closed) output(it) },
                            attached = {},
                        )
                    worker.schedule(
                        {
                            if (!closed) {
                                output(
                                    JSONObject()
                                        .put("event", "session_closed")
                                        .put(
                                            "detail",
                                            "AWG Echo: 10 минут истекли; нажмите Start Echo снова",
                                        )
                                )
                                close()
                            }
                        },
                        expires,
                        TimeUnit.MILLISECONDS,
                    )
                }
            } catch (e: Exception) {
                if (!closed)
                    output(
                        JSONObject()
                            .put("event", "operation_failed")
                            .put("detail", e.message ?: "AWG Echo unavailable")
                    )
            }
        }
    }

    @Synchronized
    fun close() {
        if (closed) return
        closed = true
        connection?.disconnect()
        session?.close()
        session = null
        worker.shutdownNow()
    }

    private fun java.io.InputStream.readBytesLimited(limit: Int): ByteArray {
        val out = java.io.ByteArrayOutputStream()
        val b = ByteArray(1024)
        while (true) {
            val n = read(b)
            if (n < 0) break
            require(out.size() + n <= limit) { "Echo config too large" }
            out.write(b, 0, n)
        }
        return out.toByteArray()
    }
}
