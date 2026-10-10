package ru.vpnc.quiclab

import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*
import org.json.JSONObject

internal interface EchoDiagnosticBackend {
    fun vpnActive(): Boolean
    fun exits(): Set<String>
    fun start(exitId: String, target: String, intervalMs: Long, output: (JSONObject) -> Unit): AutoCloseable
}

/** Owns one diagnostic lease; failures never choose a different exit or start VPN. */
internal class EchoDiagnosticController(private val backend: EchoDiagnosticBackend, private val changed: () -> Unit = {}) : AutoCloseable {
    private var lease: AutoCloseable? = null
    private var serial = 0L
    var running = false; private set
    var result: JSONObject? = null; private set
    var message = "Ожидаем запуска"; private set
    fun canOpenStandalone() = !backend.vpnActive()
    fun start(exitId: String, target: String, intervalMs: Long): Boolean {
        val match = Regex("^(\\[[^\\]\\s/\\\\]+\\]|[^:\\s/\\\\]+):(\\d+)$").matchEntire(target.trim())
        val port = match?.groupValues?.get(2)?.toIntOrNull() ?: 0
        if (match == null || port !in 1..65535 || intervalMs !in 100..60000) {
            message = "Укажите host:port и интервал 100–60000 мс"; changed(); return false
        }
        if (!backend.vpnActive() || exitId !in backend.exits()) {
            message = "Выбранный VPN-выход недоступен"; changed(); return false
        }
        close()
        val token = ++serial
        running = true; message = "Проверяем полный путь через выбранный выход"
        return try {
            lease = backend.start(exitId,target.trim(),intervalMs) { event ->
                if (running && token == serial) {
                    if (event.optBoolean("terminal")) {
                        close()
                        message = event.optString("error").ifBlank { "Диагностика завершена" }
                    } else result = JSONObject(event.toString())
                    changed()
                }
            }
            changed(); true
        } catch (e: Exception) {
            running = false; message = e.message ?: "Диагностика недоступна"; changed(); false
        }
    }
    override fun close() {
        serial++; running = false; result = null; message = "Диагностика остановлена"
        val previous = lease; lease = null
        runCatching { previous?.close() }
    }
}

class EchoActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())
    private lateinit var controller: EchoDiagnosticController
    private lateinit var exits: Spinner
    private lateinit var state: TextView
    private lateinit var start: Button
    private lateinit var stop: Button
    private var exitIds = emptyList<String>()
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        val backend = object : EchoDiagnosticBackend {
            override fun vpnActive() = LabVpnService.active
            override fun exits() = LabVpnService.echoExits().keys
            override fun start(exitId: String, target: String, intervalMs: Long, output: (JSONObject) -> Unit) =
                LabVpnService.startExitEcho(exitId,target,intervalMs) { event -> handler.post { output(event); render() } }
        }
        controller = EchoDiagnosticController(backend) { if (::state.isInitialized) render() }
        val panel = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; setPadding(24,24,24,24) }
        fun label(text: String) = TextView(this).apply { this.text = text; textSize = 16f; panel.addView(this) }
        label("Echo · два режима").textSize = 23f
        label("Самостоятельный опыт сравнивает QUIC / HTTPS / AWG и сохраняет радиографики. Перед ним остановите VPN.")
        panel.addView(Button(this).apply { text = "Самостоятельное сравнение ›"; setOnClickListener {
            if (controller.canOpenStandalone()) {
                controller.close()
                startActivity(Intent(this@EchoActivity,StandaloneEchoActivity::class.java))
            } else Toast.makeText(this@EchoActivity,"Сначала остановите VPN",Toast.LENGTH_SHORT).show()
        } })
        label("Полный путь через активный VPN-выход").textSize = 20f
        label("TCP responder должен вернуть ровно полученные 32 байта. Адрес задаёте вы; этот RTT измеряется отдельно от RTT шлюза.")
        exits = Spinner(this); panel.addView(exits)
        val target = EditText(this).apply { hint = "host:port тестового responder"; setSingleLine(true); panel.addView(this) }
        val interval = EditText(this).apply { hint = "Интервал, мс (100–60000)"; setText("1000"); inputType = android.text.InputType.TYPE_CLASS_NUMBER; panel.addView(this) }
        start = Button(this).apply { text = "Начать Echo через выход"; setOnClickListener {
            val id = exitIds.getOrNull(exits.selectedItemPosition).orEmpty()
            controller.start(id,target.text.toString(),interval.text.toString().toLongOrNull() ?: 0)
        } }; panel.addView(start)
        stop = Button(this).apply { text = "Остановить диагностику"; setOnClickListener { controller.close(); render() } }; panel.addView(stop)
        state = label("Ожидаем запуска")
        panel.addView(Button(this).apply { text = "Вернуться к VPN"; setOnClickListener { finish() } })
        setContentView(ScrollView(this).apply { addView(panel) })
    }
    override fun onResume() {
        super.onResume()
        val names = LabVpnService.echoExits(); exitIds = names.keys.toList()
        exits.adapter = ArrayAdapter(this,android.R.layout.simple_spinner_dropdown_item,
            if (exitIds.isEmpty()) listOf("Нет активных VPN-выходов") else exitIds.map { names[it] ?: it })
        render()
    }
    private fun render() {
        start.isEnabled = !controller.running
        stop.isEnabled = controller.running
        exits.isEnabled = !controller.running
        val event = controller.result
        state.text = when {
            event?.has("error") == true -> "Ошибка полного пути: ${event.optString("error")}\nVPN и выбранный выход продолжают работать"
            event?.has("rtt_ms") == true -> "RTT полного пути: %.1f мс\nЧерез выбранный выход · %s".format(event.optDouble("rtt_ms"),event.optString("target"))
            else -> controller.message
        }
    }
    override fun onStop() {
        controller.close(); handler.removeCallbacksAndMessages(null)
        super.onStop()
    }
    override fun onDestroy() { controller.close(); handler.removeCallbacksAndMessages(null); super.onDestroy() }
}
