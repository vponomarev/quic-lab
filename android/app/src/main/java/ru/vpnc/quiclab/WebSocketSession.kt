package ru.vpnc.quiclab

import android.net.Network
import android.os.ParcelFileDescriptor
import android.os.SystemClock
import mobile.EventSink
import mobile.Mobile
import mobile.SocketBinder
import mobile.WebSocketClient
import org.json.JSONObject
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit

/** Same selected Android Network as QUIC; TCP needs a new connection on change. */
internal class WebSocketSession(private val output: (JSONObject) -> Unit) {
    private val worker = Executors.newSingleThreadScheduledExecutor()
    private var client: WebSocketClient? = null
    private var network: Network? = null
    private val available = mutableMapOf<Int, Network>()
    private val validatedSince = mutableMapOf<Network, Long>()
    private var selectedAt = 0L
    private var wifiRetryAt = 0L
    private var connecting = false
    private var connected = false
    private var retryAt = 0L
    private var attempts = 0
    private var host = ""
    @Volatile private var enabled = false
    @Volatile private var closed = false
    @Volatile private var epoch = 0L
    @Volatile private var lastEcho = 0L

    init {
        worker.scheduleWithFixedDelay({
            if (!enabled || closed) return@scheduleWithFixedDelay
            val time = SystemClock.elapsedRealtime()
            if (connected && time - lastEcho > 1500) {
                event("reconnecting", "Нет ответов: новое TCP-соединение")
                resetClient()
            }
            if (!connected && !connecting && network != null && time >= retryAt) connect()
            val wifi = available[QuicSession.WIFI]
            if (wifi != null && network != wifi && connected && time >= wifiRetryAt &&
                time - selectedAt >= 8000 && validatedSince[wifi]?.let { time - it >= 5000 } == true) preferWifi(wifi)
        }, 250, 250, TimeUnit.MILLISECONDS)
    }

    private fun submit(action: () -> Unit) = synchronized(worker) { if (!closed) worker.execute(action) }
    private fun event(kind: String, detail: String) = output(JSONObject().put("event", kind).put("detail", detail))

    fun enable(hostname: String) = submit { host = hostname; enabled = true; attempts = 0 }

    fun validation(network: Network, kind: Int, valid: Boolean) = submit {
        if (valid) validatedSince.putIfAbsent(network, SystemClock.elapsedRealtime()) else validatedSince.remove(network)
    }

    fun availability(network: Network, kind: Int, present: Boolean) = submit {
        if (present) available[kind] = network
        else if (available[kind] == network) available.remove(kind)
        if (!present) validatedSince.remove(network)
        if (!enabled) return@submit
        if (!present && this.network == network) {
            resetClient()
            this.network = null
            event("waiting_network", "Выбранная сеть исчезла; ищем доступную")
        }
        if (this.network == null) {
            available.entries.firstOrNull()?.let { select(it.value, it.key) }
        }
    }

    fun select(network: Network, kind: Int) = submit {
        if (!enabled || this.network == network) return@submit
        val changing = this.network != null
        this.network = network
        selectedAt = SystemClock.elapsedRealtime()
        resetClient()
        retryAt = 0
        event("active_network", QuicSession.label(kind))
        if (changing) event("reconnecting", "Смена сети требует нового TCP/TLS-соединения")
        connect()
    }

    private fun resetClient() {
        epoch++
        client?.stop()
        client = null
        connected = false
        connecting = false
    }

    // Keep the working cellular connection until a TLS/WebSocket handshake on
    // validated Wi-Fi succeeds. QUIC may be stalled or already closed.
    private fun preferWifi(wifi: Network) {
        val token = epoch + 1
        var candidate: WebSocketClient? = null
        try {
            candidate = Mobile.newWebSocketClient(object : EventSink {
                override fun onEvent(eventJSON: String) {
                    if (closed || !enabled || token != epoch) return
                    val e = JSONObject(eventJSON)
                    if (e.optString("event") == "echo") lastEcho = SystemClock.elapsedRealtime()
                    output(e)
                    if (e.optString("event") == "disconnected") submit {
                        if (token == epoch) { resetClient(); retryAt = SystemClock.elapsedRealtime() + 500 }
                    }
                }
            })
            val resolved = resolveEndpoint("$host:443", wifi)
            candidate.start(resolved.address, host, 50, object : SocketBinder {
                override fun bind(fd: Long) { ParcelFileDescriptor.fromFd(fd.toInt()).use { wifi.bindSocket(it.fileDescriptor) } }
            })
            if (!enabled || closed) { candidate.stop(); return }
            val previous = client
            epoch = token
            client = candidate
            network = wifi
            selectedAt = SystemClock.elapsedRealtime()
            lastEcho = selectedAt
            connected = true
            event("active_network", "Wi-Fi")
            event("connected", "Wi-Fi проверен через HTTPS; новый сеанс готов")
            previous?.stop()
        } catch (e: Exception) {
            candidate?.stop()
            wifiRetryAt = SystemClock.elapsedRealtime() + 15000
            event("wifi_probe_failed", "Остаёмся на текущей сети: ${e.message}")
        }
    }

    private fun connect() {
        val selected = network ?: return
        if (!enabled) return
        resetClient()
        connecting = true
        val token = epoch
        attempts++
        event("connecting", "Подключение HTTPS · попытка $attempts")
        try {
            val current = Mobile.newWebSocketClient(object : EventSink {
                override fun onEvent(eventJSON: String) {
                    if (closed || !enabled || token != epoch) return
                    val e = JSONObject(eventJSON)
                    if (e.optString("event") == "echo") lastEcho = SystemClock.elapsedRealtime()
                    output(e)
                    if (e.optString("event") == "disconnected") submit {
                        if (token == epoch) {
                            resetClient()
                            retryAt = SystemClock.elapsedRealtime() + 500
                        }
                    }
                }
            })
            client = current
            val resolved = resolveEndpoint("$host:443", selected)
            current.start(resolved.address, host, 50, object : SocketBinder {
                override fun bind(fd: Long) {
                    ParcelFileDescriptor.fromFd(fd.toInt()).use { selected.bindSocket(it.fileDescriptor) }
                }
            })
            connected = true
            lastEcho = SystemClock.elapsedRealtime()
        } catch (e: Exception) {
            event("operation_failed", e.message ?: e.toString())
            resetClient()
            retryAt = SystemClock.elapsedRealtime() + 1500
        } finally { connecting = false }
    }

    fun stop() {
        enabled = false
        submit { enabled = false; resetClient(); network = null; event("stopped", "Остановлено") }
    }

    fun close() = synchronized(worker) {
        if (!closed) {
            closed = true
            enabled = false
            worker.execute { resetClient() }
            worker.shutdown()
        }
    }
}
