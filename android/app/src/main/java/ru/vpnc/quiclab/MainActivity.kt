package ru.vpnc.quiclab

import android.app.Activity
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.net.ConnectivityManager
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import android.util.Log
import android.view.View
import android.view.WindowInsets
import android.view.WindowManager
import android.widget.*
import org.json.JSONObject

class MainActivity : Activity() {
    private val ink = Color.rgb(20, 40, 58)
    private val muted = Color.rgb(94, 112, 127)
    private val teal = Color.rgb(0, 128, 117)
    private val amber = Color.rgb(186, 105, 17)
    private lateinit var quic: QuicSession
    private lateinit var wss: WebSocketSession
    private lateinit var radios: RadioMonitor
    private var q = TransportStats()
    private var w = TransportStats()
    private var a = TransportStats()
    private var profileEcho: ProfileEchoSession? = null
    private var publicAwg: PublicAwgEchoSession? = null
    private var echoTransports = emptySet<String>()
    private lateinit var profileCompare: CheckBox
    private lateinit var aCard: MetricCard
    private lateinit var qCard: MetricCard
    private lateinit var wCard: MetricCard
    private lateinit var banner: TextView
    private lateinit var networksView: TextView
    private lateinit var reserveView: TextView
    private lateinit var timelineView: TextView
    private lateinit var detailsView: TextView
    private lateinit var chart: LatencyChart
    private lateinit var startButton: Button
    private lateinit var vpnChoice: TextView
    private lateinit var vpnButton: Button
    private lateinit var vpnExit: TextView
    private lateinit var exitRefresh: Button
    private lateinit var vpnCard: LinearLayout
    private lateinit var echoRow: LinearLayout
    private lateinit var vpnMetrics: TextView
    private lateinit var vpnTraffic: TextView
    private lateinit var graphTitle: TextView
    private var vpnChartSession=0L
    private var vpnTransition=0L
    private var shortWifi=""
    private var shortCell=""
    private lateinit var compare: CheckBox
    private lateinit var endpoint: EditText
    private lateinit var hostname: EditText
    private lateinit var pin: EditText
    private val available = mutableSetOf<Int>()
    private val timeline = ArrayDeque<String>()
    private val raw = ArrayDeque<String>()
    private val handler = Handler(Looper.getMainLooper())
    private var running = false
    private var comparing = true
    private var experimentStart = 0L
    @Volatile private var destroyed = false

    private fun dp(value: Int) = (value * resources.displayMetrics.density).toInt()
    private fun background(color: Int, radius: Int = 18) = GradientDrawable().apply {
        setColor(color); cornerRadius = dp(radius).toFloat()
    }
    private fun text(value: String, size: Float = 14f, color: Int = ink, bold: Boolean = false) = TextView(this).apply {
        text = value; textSize = size; setTextColor(color)
        if (bold) typeface = Typeface.create("sans-serif-medium", Typeface.NORMAL)
    }
    private fun space(panel: LinearLayout, height: Int) { panel.addView(View(this), LinearLayout.LayoutParams(1, dp(height))) }
    private fun card(panel: LinearLayout): LinearLayout {
        val box = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL
            setPadding(dp(16), dp(14), dp(16), dp(14))
            background = background(Color.WHITE)
        }
        panel.addView(box, LinearLayout.LayoutParams(-1, -2).apply { bottomMargin = dp(12) })
        return box
    }
    private fun button(value: String, action: () -> Unit) = Button(this).apply {
        text = value; isAllCaps = false; textSize = 14f; setTextColor(ink)
        setOnClickListener { action() }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        Diagnostics.init(applicationContext)
        Diagnostics.event("app", JSONObject().put("event","screen_opened"))
        window.addFlags(WindowManager.LayoutParams.FLAG_KEEP_SCREEN_ON)
        window.statusBarColor = Color.rgb(240, 245, 248)
        window.navigationBarColor = Color.rgb(240, 245, 248)
        val panel = LinearLayout(this).apply {
            orientation = LinearLayout.VERTICAL; setBackgroundColor(Color.rgb(240, 245, 248))
            setPadding(dp(18), dp(16), dp(18), dp(20))
        }
        val scroll = ScrollView(this).apply { addView(panel); isFillViewport = true }
        scroll.setOnApplyWindowInsetsListener { _, insets ->
            val bars = insets.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout())
            panel.setPadding(dp(18) + bars.left, dp(16) + bars.top, dp(18) + bars.right, dp(20) + bars.bottom)
            insets
        }
        setContentView(scroll)
        panel.addView(text("NETWORK LAB  /  04", 12f, teal, true))
        space(panel, 8)
        panel.addView(text("Связь в движении", 28f, ink, true))
        panel.addView(text("Один сервер. Разные транспорты. Смена сети.", 14f, muted))
        space(panel, 16)
        banner = text("Готовы к эксперименту", 16f, ink, true)
        panel.addView(banner)
        networksView = text("Ищем доступные сети…", 13f, muted)
        panel.addView(networksView)
        reserveView = text("Запустите Echo или VPN. Кнопки Wi-Fi / Mobile меняют путь активного режима.", 13f, muted)
        panel.addView(reserveView)
        space(panel, 14)

        val controls=LinearLayout(this)
        startButton=button("Start Echo") { if(running) stopExperiment() else startExperiment() }
        vpnButton=button("Start VPN") { toggleVPN() }
        controls.addView(startButton,LinearLayout.LayoutParams(0,dp(52),1f))
        controls.addView(vpnButton,LinearLayout.LayoutParams(0,dp(52),1f))
        panel.addView(controls)
        vpnChoice=text("",12f,muted)
        panel.addView(vpnChoice)
        vpnChoice.setPadding(0,dp(8),0,dp(8))
        vpnChoice.setOnClickListener {
            if(!running) startActivity(android.content.Intent(this,VpnActivity::class.java))
        }
        val moves=LinearLayout(this)
        moves.addView(button("Wi-Fi") { move(QuicSession.WIFI) },LinearLayout.LayoutParams(0,dp(48),1f))
        moves.addView(button("Mobile") { move(QuicSession.CELLULAR) },LinearLayout.LayoutParams(0,dp(48),1f))
        panel.addView(moves)
        space(panel,12)
        vpnCard=card(panel)
        vpnCard.addView(text("VPN · канал и трафик",18f,teal,true))
        vpnMetrics=text("",15f,ink)
        vpnTraffic=text("",19f,teal,true)
        vpnCard.addView(vpnMetrics)
        space(vpnCard,8)
        vpnCard.addView(vpnTraffic)
        vpnExit=text("",16f,ink,true)
        vpnCard.addView(vpnExit)
        exitRefresh=button("Проверить exit IP") {
            startService(android.content.Intent(this,LabVpnService::class.java).setAction("exit-ip"))
        }
        vpnCard.addView(exitRefresh)
        vpnCard.addView(text("TX ↑ отправка · RX ↓ приём. Данные TCP/UDP внутри туннеля; без keep-alive и шифрования. Средняя скорость за ~1 с.",11f,muted))
        vpnCard.visibility=View.GONE
        val row = LinearLayout(this).apply { orientation = LinearLayout.HORIZONTAL }
        panel.addView(row)
        echoRow=row
        qCard = MetricCard("QUIC", "Надёжный поток · UDP", teal)
        wCard = MetricCard("HTTPS", "WebSocket · TLS/TCP", amber)
        row.addView(qCard.box, LinearLayout.LayoutParams(0, -2, 1f).apply { rightMargin = dp(5) })
        row.addView(wCard.box, LinearLayout.LayoutParams(0, -2, 1f).apply { leftMargin = dp(5) })
        aCard = MetricCard("AmneziaWG", "ICMP через туннель · 1 запрос/с", Color.rgb(100,80,175))
        panel.addView(aCard.box, LinearLayout.LayoutParams(-1,-2).apply { topMargin=dp(10) })
        aCard.box.visibility=View.GONE
        space(panel, 12)
        val graphBox = card(panel)
        graphTitle=text("Echo · задержка ответа",15f,ink,true)
        graphBox.addView(graphTitle)
        graphBox.addView(LinearLayout(this).apply {
            addView(text("● QUIC     ", 12f, teal))
            addView(text("● HTTPS    ", 12f, amber))
            addView(text("● AWG", 12f, Color.rgb(100,80,175)))
        })
        chart = LatencyChart(this)
        chart.contentDescription = "График RTT до шлюза: QUIC зелёный, HTTPS оранжевый, AWG фиолетовый. Пробел означает отсутствие свежих ответов."
        graphBox.addView(chart, LinearLayout.LayoutParams(-1, dp(100)))
        graphBox.addView(text("│ Обнаружена смена сети, точки, соты или QUIC-пути", 11f, muted))

        compare = CheckBox(this).apply {
            text = "Сравнивать с HTTPS / WebSocket"; isChecked = true; textSize = 14f; setTextColor(ink)
        }
        panel.addView(compare)
        profileCompare = CheckBox(this).apply {
            text="Echo через VPN-профиль · QUIC / HTTPS / AWG"
            textSize=14f; setTextColor(ink)
            setOnCheckedChangeListener { _, checked -> compare.isEnabled=!checked }
        }
        panel.addView(profileCompare)
        panel.addView(button("Сканировать QR") {
            if(running || LabVpnService.active) android.widget.Toast.makeText(this,"Сначала остановите опыт / VPN",android.widget.Toast.LENGTH_SHORT).show()
            else startActivityForResult(android.content.Intent(this,ProfileScanActivity::class.java),ProfileImport.REQUEST)
        })
        panel.addView(button("VPN / Exit node") {
            if (running) { android.widget.Toast.makeText(this,"Сначала остановите echo-опыт",android.widget.Toast.LENGTH_SHORT).show() }
            else startActivity(android.content.Intent(this,VpnActivity::class.java))
        })
        space(panel,10)
        val radioBox = card(panel)
        radioBox.addView(text("Сети и радиоканал", 16f, ink, true))
        val wifiDetails = text("Wi-Fi: ожидаем сведения", 12f, muted)
        val cellDetails = text("Сота: ожидаем сведения", 12f, muted)
        radioBox.addView(wifiDetails)
        space(radioBox, 8)
        radioBox.addView(cellDetails)
        radioBox.addView(text("Android требует точную геопозицию для SSID/BSSID и соты. Данные показываются локально. Обновления соты могут задерживаться.", 11f, muted))
        radioBox.addView(button("Разрешить сведения о сетях") {
            requestPermissions(arrayOf(android.Manifest.permission.ACCESS_COARSE_LOCATION, android.Manifest.permission.ACCESS_FINE_LOCATION), 42)
        })
        radios = RadioMonitor(this, { wifi, cell -> wifiDetails.text = wifi; cellDetails.text = cell; shortWifi=wifi.lineSequence().first().take(58); shortCell=cell.lineSequence().drop(1).firstOrNull()?.take(48) ?: "Сота: нет данных" }, { message, time ->
            Diagnostics.event("radio",JSONObject().put("event","radio_transition").put("detail",if(message.startsWith("Wi-Fi")) "Wi-Fi AP changed" else "Serving cell changed"))
            if (running || LabVpnService.active) {
                chart.mark(time)
                timeline.addFirst("%5.1f с  %s".format((time - experimentStart) / 1000.0, message))
                while (timeline.size > 8) timeline.removeLast()
                timelineView.text = timeline.joinToString("\n\n")
            }
        })
        val help = card(panel)
        help.addView(text("Что наблюдать", 16f, ink, true))
        help.addView(text("1. Начните на Wi-Fi и дождитесь ответов.\n2. Выключите Wi-Fi: посмотрите на паузу и сеансы.\n3. Включите обратно. Возврат после проверки сети занимает около 8 секунд — обмен продолжается.", 14f, muted))
        space(help, 8)
        help.addView(text("RTT — путь сообщения туда и обратно. При транзите: VPN / удалённый gateway. График и jitter показывают только RTT до VPN. Пауза — время между ответами, включая переподключение. Сеанс — соединение, подтверждённое сервером.", 12f, muted))
        help.addView(text("Jitter max — максимальная разница RTT соседних ответов за последние 15 секунд. Пик перехода исчезнет через 15 секунд. Если ответов меньше двух — прочерк; замирание видно по паузе.", 12f, muted))
        val events = card(panel)
        events.addView(text("Ход эксперимента", 16f, ink, true))
        timelineView = text("Здесь появятся события смены сети.", 13f, muted)
        events.addView(timelineView)
        panel.addView(text("Публичный Echo: 20 сообщений/с. Echo VPN-профиля: QUIC/HTTPS 10/с, AWG ICMP 1/с. AWG требует ключ профиля; график и jitter сравнивают разные виды проб. При смене сети WSS открывает новый сеанс; может восстановиться и вернуться на Wi-Fi независимо от QUIC. Это демонстрация механики, не тест максимальной скорости.", 12f, muted))
        space(panel, 10)

        panel.addView(button("Поделиться диагностикой") {
            Diagnostics.preview(this, "Echo QUIC: replies=${q.replies}, migrations=${q.migrations}, RTT=${q.rtt}, maxGap=${q.maxGap}\nEcho HTTPS: replies=${w.replies}, RTT=${w.rtt}, maxGap=${w.maxGap}\nEcho AWG: replies=${a.replies}, RTT=${a.rtt}; transit RTT: QUIC=${q.transitRtt}, HTTPS=${w.transitRtt}, AWG=${a.transitRtt}")
        })
        val preferences = getSharedPreferences("server", MODE_PRIVATE)
        val settings = LinearLayout(this).apply { orientation = LinearLayout.VERTICAL; visibility = if (preferences.getString("endpoint", "").isNullOrBlank()) View.VISIBLE else View.GONE }
        panel.addView(button("Настройки стенда и подробности") { settings.visibility = if (settings.visibility == View.GONE) View.VISIBLE else View.GONE })
        panel.addView(settings)
        fun field(label: String, value: String): EditText {
            settings.addView(text(label, 12f, muted))
            return EditText(this).apply { setText(value); isSingleLine = true; textSize = 13f; settings.addView(this) }
        }
        endpoint = field("QUIC сервер:порт", preferences.getString("endpoint", "") ?: "").apply { hint = "quic.example.org:4433" }
        hostname = field("TLS имя / HTTPS домен", preferences.getString("hostname", "") ?: "").apply { hint = "quic.example.org" }
        pin = field("Pin только для локального QUIC стенда", preferences.getString("pin", "") ?: "")
        compare.isChecked = preferences.getBoolean("compare", true)
        detailsView = text("Диагностика появится после запуска.", 11f, muted).apply { setTextIsSelectable(true) }
        settings.addView(detailsView)
        wss = WebSocketSession { accept(false, it) }
        quic = QuicSession(getSystemService(ConnectivityManager::class.java), { network, kind ->
            wss.select(network, kind)
        }, { network, kind, present -> wss.availability(network, kind, present) },
            { network, kind, valid -> wss.validation(network, kind, valid) }) { accept(true, it) }
        handler.post(refresh)
        radios.start()
    }

    private inner class MetricCard(private val title: String, subtitle: String, color: Int) {
        val box = LinearLayout(this@MainActivity).apply {
            orientation = LinearLayout.VERTICAL; setPadding(dp(12), dp(14), dp(12), dp(14)); background = background(Color.WHITE)
        }
        private val status = text("Готов к запуску", 12f, muted)
        private val rtt = text("—", 29f, color, true)
        private val gap = text("Пауза макс.   —", 12f, muted)
        private val jitter = text("—", 21f, color, true)
        private val sessions = text("Сеансов   0", 14f, ink, true)
        private val network = text("Сеть   —", 12f, muted)
        init {
            box.addView(text(title, 20f, color, true)); box.addView(text(subtitle, 10f, muted))
            space(box, 9); box.addView(status); box.addView(rtt); box.addView(text("RTT, мс · шлюз / транзит", 11f, muted))
            space(box, 8); box.addView(text("Jitter max · 15 с", 11f, muted)); box.addView(jitter)
            space(box, 8); box.addView(gap); space(box, 6); box.addView(sessions); box.addView(network)
        }
        fun render(model: TransportStats, now: Long, enabled: Boolean = true, freshness: Long = 1500) {
            status.text = if (!enabled) "Сравнение выключено" else if (model.silence(now) > freshness) "Нет свежих ответов" else model.state
            rtt.text = if (model.replies == 0L || model.silence(now) > freshness) "—" else "%.0f".format(model.rtt)
            if (model.transitEnabled) {
                val remote=if(model.lastTransitEcho>0 && now-model.lastTransitEcho<3500) "%.0f".format(model.transitRtt) else "—"
                rtt.append(" / $remote")
                rtt.textSize=23f
            } else rtt.textSize=29f
            gap.text = "Пауза макс.  ${if (model.replies > 1) "%.0f мс".format(maxOf(model.maxGap, model.silence(now).toDouble())) else "—"}"
            jitter.text = model.maxJitter(now)?.let { "%.1f мс".format(it) } ?: "—"
            sessions.text = if(title=="AmneziaWG") "Проб ICMP   ${model.replies}" else "Сеансов   ${model.sessions.size}"
            network.text = model.network.replace("LTE/Cellular", "Мобильная")
            box.alpha = if (enabled) 1f else 0.45f
        }
    }

    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode:Int,resultCode:Int,data:android.content.Intent?) {
        super.onActivityResult(requestCode,resultCode,data)
        if(requestCode==51 && resultCode==RESULT_OK) { startForegroundService(android.content.Intent(this,LabVpnService::class.java)); return }
        if(requestCode==ProfileImport.REQUEST && resultCode==RESULT_OK) {
            if(data?.getStringExtra("kind")=="vpn") startActivity(android.content.Intent(this,VpnActivity::class.java))
            else {val p=getSharedPreferences("server",MODE_PRIVATE);endpoint.setText(p.getString("endpoint",""));hostname.setText(p.getString("hostname",""));pin.setText(p.getString("pin",""));compare.isChecked=true}
        }
    }

    private fun startExperiment() {
        if (LabVpnService.active) { android.widget.Toast.makeText(this,"Сначала остановите VPN",android.widget.Toast.LENGTH_SHORT).show(); return }

        val kind = if (QuicSession.WIFI in available) QuicSession.WIFI else QuicSession.CELLULAR
        if (kind !in available) { Toast.makeText(this, "Подключите Wi-Fi или мобильную сеть", Toast.LENGTH_SHORT).show(); return }
        if (!profileCompare.isChecked && compare.isChecked && hostname.text.isBlank()) { Toast.makeText(this, "Для HTTPS нужен публичный домен", Toast.LENGTH_SHORT).show(); return }
        if (!profileCompare.isChecked && endpoint.text.isBlank()) { Toast.makeText(this, "Укажите сервер в настройках стенда ниже", Toast.LENGTH_LONG).show(); return }
        getSharedPreferences("server", MODE_PRIVATE).edit()
            .putString("endpoint", endpoint.text.toString().trim()).putString("hostname", hostname.text.toString().trim())
            .putString("pin", pin.text.toString().trim()).putBoolean("compare", compare.isChecked).apply()
        val selectedProfile = if(profileCompare.isChecked) try {
            ProfileEchoSession(this) { transport, event -> if(transport=="awg") acceptAwg(event) else accept(transport=="quic", event) }
        } catch(e:Exception) { Toast.makeText(this,e.message ?: "Не удалось открыть профиль",Toast.LENGTH_LONG).show(); return } else null
        profileEcho=selectedProfile
        echoTransports=selectedProfile?.transports ?: setOf("quic", "https")
        val publicEnroll=getSharedPreferences("server",MODE_PRIVATE).getString("awg_enroll_url", "").orEmpty()
        if(selectedProfile==null && publicEnroll.isNotBlank()) echoTransports=echoTransports+"awg"
        q = TransportStats(); w = TransportStats(); a = TransportStats(); timeline.clear(); raw.clear(); chart.clear()
        if(selectedProfile!=null && !VpnProfiles.preferences(this).getString("transit_endpoint", "").isNullOrBlank()) {
            q.transitEnabled=true; w.transitEnabled=true; a.transitEnabled=true
        }
        running = true; comparing = if(selectedProfile!=null) "https" in echoTransports else compare.isChecked; compare.isEnabled = false; profileCompare.isEnabled=false
        experimentStart = SystemClock.elapsedRealtime()
        endpoint.isEnabled = false; hostname.isEnabled = false; pin.isEnabled = false
        startButton.text = "Stop Echo"
        banner.text = "Эксперимент идёт"
        q.state = "Подключение…"; q.active = true
        if(selectedProfile!=null) {
            q.active="quic" in echoTransports; w.active="https" in echoTransports; a.active="awg" in echoTransports
            selectedProfile.move(kind)
            return
        }
        if(publicEnroll.isNotBlank()) {
            a.active=true
            publicAwg=PublicAwgEchoSession(this,publicEnroll) { acceptAwg(it) }
            publicAwg?.move(kind)
        }
        if (comparing) { w.state = "Ждём выбранную сеть"; wss.enable(hostname.text.toString().trim()) }
        quic.startOrMigrate(kind, endpoint.text.toString().trim(), hostname.text.toString().trim(), pin.text.toString().trim(), 50)
    }
    private fun stopExperiment() {
        running = false; quic.stop(); wss.stop(); profileEcho?.close(); profileEcho=null; publicAwg?.close(); publicAwg=null
        val time=SystemClock.elapsedRealtime()
        listOf(q,w,a).forEach { it.accept(JSONObject().put("event","stopped"),time) }
        profileCompare.isEnabled=true; compare.isEnabled = !profileCompare.isChecked; endpoint.isEnabled = true; hostname.isEnabled = true; pin.isEnabled = true
        startButton.text = "Start Echo"; banner.text = "Опыт завершён · результаты сохранены"
        reserveView.text = "Сравните число сеансов и максимальную паузу."
    }
    private fun toggleVPN() {
        if(LabVpnService.active) { startService(android.content.Intent(this,LabVpnService::class.java).setAction("stop")); return }
        if(running) { Toast.makeText(this,"Сначала остановите Echo",Toast.LENGTH_SHORT).show(); return }
        try {
            if(VpnProfiles.multiple(this)) MultipleVpnPlan.load(this) else {
            val p=VpnProfiles.preferences(this)
            require(!p.getString("endpoint","").isNullOrBlank()) { "Импортируйте VPN-профиль или заполните настройки" }
            VpnIdentity.load(this)
            }
            val consent=android.net.VpnService.prepare(this)
            if(consent!=null) startActivityForResult(consent,51)
            else startForegroundService(android.content.Intent(this,LabVpnService::class.java))
        } catch(e:Exception) {
            Toast.makeText(this,e.message,Toast.LENGTH_LONG).show()
            startActivity(android.content.Intent(this,VpnActivity::class.java))
        }
    }
    private fun size(bytes:Double):String = when {
        bytes>=1048576 -> "%.1f MiB".format(bytes/1048576)
        bytes>=1024 -> "%.1f KiB".format(bytes/1024)
        else -> "%.0f B".format(bytes)
    }
    private fun move(kind: Int) {
        if(LabVpnService.active) {
            startService(android.content.Intent(this,LabVpnService::class.java).setAction("move").putExtra("network",kind))
            return
        }
        if (!running) { Toast.makeText(this, "Сначала начните опыт", Toast.LENGTH_SHORT).show(); return }
        if (kind !in available) { Toast.makeText(this, "Эта сеть сейчас недоступна", Toast.LENGTH_SHORT).show(); return }
        if(profileEcho!=null) { profileEcho?.move(kind); return }
        publicAwg?.move(kind)
        quic.startOrMigrate(kind, endpoint.text.toString().trim(), hostname.text.toString().trim(), pin.text.toString().trim(), 50)
    }
    private fun acceptAwg(e: JSONObject) {
        Diagnostics.event("echo-awg",e)
        val time=SystemClock.elapsedRealtime()
        runOnUiThread {
            if(destroyed) return@runOnUiThread
            a.accept(e,time)
            if(running && e.optString("event") in listOf("migration_started","path_switched")) chart.mark(time)
        }
    }
    private fun accept(isQuic: Boolean, e: JSONObject) {
        Diagnostics.event(if(isQuic) "echo-quic" else "echo-https",e)
        val time = SystemClock.elapsedRealtime()
        Log.i(if (isQuic) "QuicLab" else "WssLab", e.toString())
        runOnUiThread {
            if (destroyed) return@runOnUiThread
            val model = if (isQuic) q else w
            model.accept(e, time)
            val kind = e.optString("event")
            val detail = e.optString("detail")
            if (running && isQuic && kind in listOf("network_available", "network_lost", "path_switched")) chart.mark(time)
            if (isQuic && kind in listOf("network_available", "network_lost")) {
                val type = if (detail.startsWith("Wi-Fi")) QuicSession.WIFI else QuicSession.CELLULAR
                if (kind == "network_available") available.add(type) else available.remove(type)
            }
            if (isQuic && kind == "standby_ready" && running) reserveView.text = "Резерв проверен · готов к смене сети"
            if (isQuic && kind == "standby_unavailable" && running) reserveView.text = "Резерв пока не отвечает · проверяем повторно"
            if (kind != "echo") {
                raw.addLast("${if (isQuic) "QUIC" else "WSS"} $e")
                while (raw.size > 35) raw.removeFirst()
                val message = when (kind) {
                    "active_network" -> "${if (isQuic) "QUIC" else "HTTPS"}: ${detail.replace("LTE/Cellular", "мобильная сеть")}"
                    "path_switched" -> "QUIC: путь изменён, проверяем продолжение сеанса"
                    "connected" -> "${if (isQuic) "QUIC" else "HTTPS"}: соединение открыто"
                    "reconnecting" -> "HTTPS: открываем новое соединение"
                    "disconnected", "session_closed" -> "${if (isQuic) "QUIC" else "HTTPS"}: соединение закрыто"
                    "operation_failed", "auto_migration_failed" -> "${if (isQuic) "QUIC" else "HTTPS"}: ${detail.take(100)}"
                    "waiting_network" -> "QUIC: ожидаем доступную сеть"
                    "network_available" -> "Доступна сеть: $detail"
                    "network_lost" -> "Потеряна сеть: $detail"
                    else -> null
                }
                if (message != null && experimentStart > 0) {
                    timeline.addFirst("%5.1f с  %s".format((time - experimentStart) / 1000.0, message))
                    while (timeline.size > 8) timeline.removeLast()
                    timelineView.text = timeline.joinToString("\n\n")
                }
            }
        }
    }
    private val refresh = object : Runnable {
        override fun run() {
            if (destroyed) return
            val time = SystemClock.elapsedRealtime()
            val vpn=LabVpnService.active || (!running && LabVpnService.startedAt>0)
            vpnChoice.text="${VpnProfiles.current(this@MainActivity).name} · VPN: ${if(LabVpnService.active) LabVpnService.transport.uppercase() else VpnProfiles.preferences(this@MainActivity).getString("transport","quic")!!.uppercase()} · Настроить ›"
            if(VpnProfiles.multiple(this@MainActivity)) vpnChoice.text="Multiple · ${VpnProfiles.enabled(this@MainActivity).size} профилей · Настроить ›"
            vpnButton.text=if(LabVpnService.active) "Stop VPN" else "Start VPN"
            vpnButton.isEnabled=!running
            startButton.isEnabled=!LabVpnService.active
            echoRow.visibility=if(vpn) View.GONE else View.VISIBLE
            vpnCard.visibility=if(vpn) View.VISIBLE else View.GONE
            compare.visibility=if(vpn || profileCompare.isChecked) View.GONE else View.VISIBLE
            profileCompare.visibility=if(vpn) View.GONE else View.VISIBLE
            aCard.box.visibility=if(!vpn && "awg" in echoTransports) View.VISIBLE else View.GONE
            graphTitle.text=if(vpn) "VPN · RTT до локального шлюза" else "Echo · задержка ответа"
            if(vpn) {
                val fresh=LabVpnService.active && LabVpnService.lastEcho>0 && time-LabVpnService.lastEcho<1500
                val localRtt=if(fresh) "%.0f".format(LabVpnService.rtt) else "—"
                val transitRtt=if(LabVpnService.active && LabVpnService.lastTransitEcho>0 && time-LabVpnService.lastTransitEcho<3500) "%.0f".format(LabVpnService.transitRtt) else "—"
                val rtt=if(LabVpnService.transitEnabled) "$localRtt / $transitRtt мс · VPN / транзит" else "$localRtt мс"
                val transport=LabVpnService.transport.uppercase()
                vpnMetrics.text="$transport · ${LabVpnService.network}\nRTT: $rtt\n${LabVpnService.quality(time)}\n${LabVpnService.flowSummary}"
                vpnExit.visibility=if(LabVpnService.exitEnabled) View.VISIBLE else View.GONE
                exitRefresh.visibility=if(LabVpnService.exitEnabled) View.VISIBLE else View.GONE
                exitRefresh.isEnabled=LabVpnService.active
                val age=if(LabVpnService.exitCheckedAt>0) " · ${(time-LabVpnService.exitCheckedAt)/1000} с назад" else ""
                val exitStatus=if(LabVpnService.active && LabVpnService.lastEcho>0 && time-LabVpnService.lastEcho>2500) "Нет свежих ответов туннеля" else LabVpnService.exitState
                vpnExit.text="Exit IPv4: ${LabVpnService.exitIP.ifBlank { "—" }}\n$exitStatus$age"
                vpnTraffic.text="TX ↑ ${size(LabVpnService.txRate)}/с    RX ↓ ${size(LabVpnService.rxRate)}/с\nВсего: ↑ ${size(LabVpnService.txBytes.toDouble())}    ↓ ${size(LabVpnService.rxBytes.toDouble())}"
                banner.text=if(LabVpnService.active && !fresh) "VPN · ждём ответы" else LabVpnService.status
                reserveView.text=if(transport=="AWG") "ICMP endpoint через AWG · 1 запрос/с · ${LabVpnService.network}" else "Keep-alive: 10 запросов/с · $transport · ${LabVpnService.network}"
                if(LabVpnService.multipleMode) {
                    vpnMetrics.text=MultipleVpnState.summary()
                    vpnTraffic.text="Статистика показана отдельно для каждого профиля"
                    banner.text=LabVpnService.status
                    reserveView.text="Multiple · управление профилями в настройках VPN"
                }
                if(vpnChartSession!=LabVpnService.startedAt) { chart.clear(); vpnChartSession=LabVpnService.startedAt }
                if(vpnTransition!=LabVpnService.lastTransition) { vpnTransition=LabVpnService.lastTransition; chart.mark(time) }
                if(LabVpnService.active) chart.sample(if(fresh && transport=="QUIC") LabVpnService.rtt.toFloat() else null,if(fresh && transport!="QUIC") LabVpnService.rtt.toFloat() else null)
                timelineView.text=LabVpnService.log()
            }
            qCard.render(q, time, !profileCompare.isChecked || "quic" in echoTransports); wCard.render(w, time, comparing); aCard.render(a,time,"awg" in echoTransports,2500)
            networksView.text = "Wi-Fi: ${if (QuicSession.WIFI in available) "доступен" else "нет"}    ·    Мобильная: ${if (QuicSession.CELLULAR in available) "доступна" else "нет"}"
            if(shortWifi.isNotBlank() && QuicSession.WIFI in available) networksView.append("\n$shortWifi")
            if(shortCell.isNotBlank() && QuicSession.CELLULAR in available) networksView.append("\n$shortCell")
            profileCompare.text="Echo через VPN-профиль · ${VpnProfiles.current(this@MainActivity).name}"
            if (running) {
                banner.text = if (!q.active) "QUIC: ${q.state}" else if (q.silence(time) > 400) "Сеть меняется · ждём ответы" else "Эксперимент идёт · ${q.network.replace("LTE/Cellular", "мобильная сеть")}"
                if(profileEcho!=null) banner.text="Сравнение: ${echoTransports.joinToString(" / ") { it.uppercase() }}"
                chart.sample(if (q.active && q.lastEcho != 0L && q.silence(time) < 500) q.rtt.toFloat() else null,
                    if (comparing && w.active && w.lastEcho != 0L && w.silence(time) < 500) w.rtt.toFloat() else null,
                    if(a.active && a.lastEcho!=0L && a.silence(time)<2500) a.rtt.toFloat() else null)
            }
            if (detailsView.isShown) detailsView.text = "QUIC: ${q.replies} ответов, ${q.migrations} миграций\nСеансы: ${q.sessions.joinToString()}\nHTTPS: ${w.replies} ответов\nСеансы: ${w.sessions.joinToString()}\n\n${raw.joinToString("\n")}"
            handler.postDelayed(this, 250)
        }
    }
    override fun onDestroy() {
        destroyed = true; handler.removeCallbacksAndMessages(null)
        radios.close(); quic.close(); wss.close(); profileEcho?.close(); publicAwg?.close(); super.onDestroy()
    }
}
