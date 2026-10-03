package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.graphics.Color
import android.net.VpnService
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.widget.*

class VpnActivity : Activity() {
    private val accent = Color.rgb(0, 122, 255)
    private val ink = Color.rgb(28, 28, 30)
    private val secondary = Color.rgb(112, 112, 117)

    private fun dp(n: Int) = (n * resources.displayMetrics.density).toInt()

    private fun rounded(color: Int) =
        android.graphics.drawable.GradientDrawable().apply {
            setColor(color)
            cornerRadius = dp(14).toFloat()
        }

    private val handler = Handler(Looper.getMainLooper())
    private lateinit var dashboardBudget: TextView
    private lateinit var dashboardExits: LinearLayout
    private lateinit var startControl: Button
    private lateinit var stopControl: Button
    private val exitViews = linkedMapOf<String, TextView>()
    private var dashboardRunId: String? = null
    private val profileLabelsByExit = mutableMapOf<String, Pair<Long, Map<String, String>>>()
    private var radios: RadioMonitor? = null
    private var wifiRadio = "Недоступно"
    private var cellRadio = "Недоступно"
    private lateinit var transport: Spinner
    private lateinit var mode: Spinner
    private lateinit var endpoint: EditText
    private lateinit var hostname: EditText
    private lateinit var routes: EditText
    private lateinit var dns: EditText
    private lateinit var ca: EditText
    private lateinit var identity: TextView
    private lateinit var state: TextView
    private lateinit var logs: TextView
    private lateinit var useGlobalApps: CheckBox
    private lateinit var appsSummary: TextView
    private var apps = mutableSetOf<String>()
    private val prefs by lazy { VpnProfiles.preferences(this) }
    private val transportTypes by lazy {
        val fallback = if (prefs.getString("transport", "quic") == "awg") setOf("awg") else setOf("quic", "https")
        val available = prefs.getStringSet("available_transports", fallback) ?: fallback
        listOf("quic", "https", "awg").filter { it in available }.ifEmpty { fallback.toList() }
    }
    private fun selectedTransport() = transportTypes[transport.selectedItemPosition]
    private fun transportName(value: String) = when(value) { "quic" -> "QUIC"; "https" -> "HTTPS / WebSocket"; else -> "AmneziaWG" }

    private fun label(panel: LinearLayout, value: String): TextView =
        TextView(this).also {
            it.text = value
            it.textSize = 13f
            it.setTextColor(secondary)
            it.setPadding(0, dp(8), 0, dp(5))
            panel.addView(it)
        }

    private fun field(
        panel: LinearLayout,
        title: String,
        key: String,
        default: String = "",
    ): EditText {
        label(panel, title)
        return EditText(this).also {
            it.setText(prefs.getString(key, default))
            it.textSize = 16f
            it.setTextColor(ink)
            it.backgroundTintList =
                android.content.res.ColorStateList.valueOf(Color.rgb(220, 220, 225))
            it.setPadding(0, dp(8), 0, dp(10))
            it.setSingleLine(key != "routes" && key != "ca")
            panel.addView(it)
        }
    }

    private fun choice(
        panel: LinearLayout,
        title: String,
        items: Array<String>,
        selection: Int,
    ): Spinner {
        label(panel, title)
        return Spinner(this).also {
            it.adapter = ArrayAdapter(this, android.R.layout.simple_spinner_dropdown_item, items)
            it.setSelection(selection)
            it.minimumHeight = dp(48)
            panel.addView(it)
        }
    }

    private fun button(panel: LinearLayout, title: String, action: () -> Unit) {
        panel.addView(
            Button(this).apply {
                stateListAnimator = null
                elevation = 0f
                text = title
                isAllCaps = false
                textSize = 16f
                setTextColor(accent)
                gravity = android.view.Gravity.CENTER_VERTICAL or android.view.Gravity.START
                minHeight = dp(48)
                setPadding(0, dp(8), 0, dp(8))
                background =
                    android.graphics.drawable.StateListDrawable().apply {
                        addState(
                            intArrayOf(android.R.attr.state_pressed),
                            rounded(Color.rgb(240, 245, 255)),
                        )
                        addState(intArrayOf(), rounded(Color.WHITE))
                    }
                setOnClickListener {
                    try {
                        action()
                    } catch (e: Exception) {
                        error(e)
                    }
                }
            }
        )
    }

    private fun error(e: Exception) {
        AlertDialog.Builder(this)
            .setTitle("QUIC Lab VPN")
            .setMessage(e.message ?: e.toString())
            .setPositiveButton("OK", null)
            .show()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        window.statusBarColor = Color.WHITE
        window.navigationBarColor = Color.rgb(242, 242, 247)
        window.decorView.systemUiVisibility =
            android.view.View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR or
                android.view.View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR
        val outer =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setBackgroundColor(Color.rgb(242, 242, 247))
            }
        val toolbar =
            LinearLayout(this).apply {
                gravity = android.view.Gravity.CENTER_VERTICAL
                setBackgroundColor(Color.WHITE)
                setPadding(dp(12), 0, dp(12), 0)
            }
        toolbar.addView(
            Button(this).apply {
                stateListAnimator = null
                elevation = 0f
                text = "‹"
                textSize = 32f
                setTextColor(accent)
                background = rounded(Color.WHITE)
                contentDescription = "Назад"
                setOnClickListener { finish() }
            },
            LinearLayout.LayoutParams(dp(48), dp(56)),
        )
        toolbar.addView(
            TextView(this).apply {
                text = "VPN"
                gravity = android.view.Gravity.CENTER_VERTICAL
                textSize = 21f
                setTextColor(ink)
                setTypeface(null, android.graphics.Typeface.BOLD)
            },
            LinearLayout.LayoutParams(0, dp(56), 1f).apply {
                gravity = android.view.Gravity.CENTER_VERTICAL
            },
        )
        toolbar.addView(
            Button(this).apply {
                stateListAnimator = null
                elevation = 0f
                text = "⋮"
                textSize = 26f
                setTextColor(ink)
                background = rounded(Color.WHITE)
                contentDescription = "Действия с профилем"
                setOnClickListener { anchor ->
                    PopupMenu(this@VpnActivity, anchor).apply {
                        menu.add("Добавить профиль")
                        menu.add("Переименовать профиль")
                        menu.add("Удалить профиль")
                        setOnMenuItemClickListener { item ->
                            try {
                                when (item.title.toString()) {
                                    "Добавить профиль" -> profileNameDialog(false)
                                    "Переименовать профиль" -> profileNameDialog(true)
                                    "Удалить профиль" -> deleteProfile()
                                }
                            } catch (e: Exception) {
                                error(e)
                            }
                            true
                        }
                        show()
                    }
                }
            },
            LinearLayout.LayoutParams(dp(48), dp(56)),
        )
        outer.addView(toolbar)
        val root =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(16), 0, dp(16), dp(24))
            }
        val scroll =
            ScrollView(this).apply {
                addView(root)
                isFillViewport = true
            }
        outer.addView(scroll, LinearLayout.LayoutParams(-1, 0, 1f))
        setContentView(outer)
        outer.setOnApplyWindowInsetsListener { _, ins ->
            val bars = ins.getInsets(android.view.WindowInsets.Type.systemBars())
            outer.setPadding(bars.left, bars.top, bars.right, bars.bottom)
            ins
        }
        fun section(title: String): LinearLayout {
            root.addView(
                TextView(this).apply {
                    text = title
                    textSize = 12f
                    setTextColor(secondary)
                    setPadding(dp(12), dp(24), dp(12), dp(8))
                }
            )
            return LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(dp(16), dp(10), dp(16), dp(10))
                background = rounded(Color.WHITE)
                root.addView(this, LinearLayout.LayoutParams(-1, -2))
            }
        }
        val home = section("VPN")
        dashboardBudget = label(home, "Общий LTE бюджет: VPN выключен")
        startControl = Button(this).apply {
            text = "Запустить VPN"; isAllCaps = false; setTextColor(accent)
            setOnClickListener {
                try {
                    if (LabVpnService.active) return@setOnClickListener
                    if (VpnProfiles.multiple(this@VpnActivity)) MultipleVpnPlan.load(this@VpnActivity) else save()
                    val request = VpnService.prepare(this@VpnActivity)
                    if (request != null) startActivityForResult(request, 12) else startVPN()
                } catch (e: Exception) { error(e) }
            }
        }
        stopControl = Button(this).apply {
            text = "Остановить VPN"; isAllCaps = false; setTextColor(accent)
            setOnClickListener { startService(Intent(this@VpnActivity, LabVpnService::class.java).setAction("stop")) }
        }
        home.addView(startControl); home.addView(stopControl)
        button(home, "Echo · диагностика сетей ›") { startActivity(Intent(this, EchoActivity::class.java)) }
        button(home, "Разрешить сведения о сети") {
            requestPermissions(arrayOf(android.Manifest.permission.ACCESS_COARSE_LOCATION, android.Manifest.permission.ACCESS_FINE_LOCATION), 21)
        }
        dashboardExits = section("АКТИВНЫЕ ВЫХОДЫ")
        var panel = section("ПРОФИЛЬ")
        button(panel, "Несколько VPN одновременно ›") { startActivity(Intent(this, MultipleVpnActivity::class.java)) }
        val profiles = VpnProfiles.list(this)
        val selectedProfile = VpnProfiles.current(this)
        val profileChoice =
            choice(
                panel,
                "Профиль сервера",
                profiles.map { it.name }.toTypedArray(),
                profiles.indexOfFirst { it.id == selectedProfile.id },
            )
        profileChoice.adapter =
            object : BaseAdapter() {
                override fun getCount() = profiles.size

                override fun getItem(position: Int) = profiles[position]

                override fun getItemId(position: Int) = position.toLong()

                private fun row(position: Int): android.view.View {
                    val profile = profiles[position]
                    val settings = VpnProfiles.preferences(this@VpnActivity, profile.id)
                    val selected = profile.id == selectedProfile.id
                    return LinearLayout(this@VpnActivity).apply {
                        gravity = android.view.Gravity.CENTER_VERTICAL
                        setPadding(0, dp(10), dp(8), dp(10))
                        setBackgroundColor(Color.WHITE)
                        addView(
                            android.view.View(this@VpnActivity).apply {
                                setBackgroundColor(if (selected) accent else Color.TRANSPARENT)
                            },
                            LinearLayout.LayoutParams(dp(3), dp(54)),
                        )
                        addView(
                            LinearLayout(this@VpnActivity).apply {
                                orientation = LinearLayout.VERTICAL
                                setPadding(dp(12), 0, dp(8), 0)
                                addView(
                                    TextView(this@VpnActivity).apply {
                                        text = profile.name
                                        textSize = 17f
                                        setTextColor(ink)
                                        setTypeface(null, android.graphics.Typeface.BOLD)
                                    }
                                )
                                addView(
                                    TextView(this@VpnActivity).apply {
                                        text =
                                            settings.getString("endpoint", "").orEmpty().ifBlank {
                                                "Сервер не настроен"
                                            }
                                        textSize = 13f
                                        setTextColor(secondary)
                                    }
                                )
                                addView(
                                    TextView(this@VpnActivity).apply {
                                        text =
                                            if (settings.getString("transport", "quic") == "quic")
                                                "QUIC"
                                            else if (settings.getString("transport", "quic") == "awg") "AmneziaWG" else "HTTPS / WebSocket"
                                        textSize = 12f
                                        setTextColor(accent)
                                    }
                                )
                            },
                            LinearLayout.LayoutParams(0, -2, 1f),
                        )
                    }
                }

                override fun getView(
                    position: Int,
                    convertView: android.view.View?,
                    parent: android.view.ViewGroup,
                ) = row(position)

                override fun getDropDownView(
                    position: Int,
                    convertView: android.view.View?,
                    parent: android.view.ViewGroup,
                ) = row(position)
            }
        profileChoice.setSelection(profiles.indexOfFirst { it.id == selectedProfile.id })
        profileChoice.isEnabled = !LabVpnService.active
        profileChoice.onItemSelectedListener =
            object : AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent: AdapterView<*>?) {}

                override fun onItemSelected(
                    parent: AdapterView<*>?,
                    view: android.view.View?,
                    position: Int,
                    id: Long,
                ) {
                    if (profiles[position].id != selectedProfile.id) {
                        try {
                            VpnProfiles.select(this@VpnActivity, profiles[position].id)
                            recreate()
                        } catch (e: Exception) {
                            error(e)
                            profileChoice.setSelection(profiles.indexOf(selectedProfile))
                        }
                    }
                }
            }
        label(panel, "Сохраните изменения перед переключением сервера.")
        button(panel, "Сканировать QR") {
            check(!LabVpnService.active) { "Сначала остановите VPN" }
            startActivityForResult(
                Intent(this, ProfileScanActivity::class.java),
                ProfileImport.REQUEST,
            )
        }
        button(panel, "Импортировать профиль .json / .conf") {
            check(!LabVpnService.active) { "Сначала остановите VPN" }
            startActivityForResult(Intent(Intent.ACTION_OPEN_DOCUMENT).setType("*/*").addCategory(Intent.CATEGORY_OPENABLE), 14)
        }
        button(panel, "Обновить конфиг") { updateProfile(VpnProfiles.current(this).id) }
        state = label(panel, LabVpnService.status)
        state.setTextColor(accent)
        panel = section("ПОДКЛЮЧЕНИЕ")
        transport =
            choice(
                panel,
                "Транспорт",
                transportTypes.map { transportName(it) }.toTypedArray(),
                transportTypes.indexOf(prefs.getString("transport", "quic")).coerceAtLeast(0),
            )
        endpoint = field(panel, "Сервер · домен:порт", "endpoint")
        hostname = field(panel, "Имя сервера в сертификате TLS", "hostname")
        hostname.isEnabled = selectedTransport() != "awg"
        label(panel, "AmneziaWG: импорт .conf или QR. RTT — ICMP ping endpoint через туннель; частота задаётся в диагностике. Доступные протоколы задаются при выдаче профиля.")
        transport.onItemSelectedListener =
            object : android.widget.AdapterView.OnItemSelectedListener {
                private var initial = true

                override fun onNothingSelected(parent: android.widget.AdapterView<*>?) {}

                override fun onItemSelected(
                    parent: android.widget.AdapterView<*>?,
                    view: android.view.View?,
                    position: Int,
                    id: Long,
                ) {
                    hostname.isEnabled = transportTypes[position] != "awg" && !prefs.getBoolean("managed_profile",false)
                    if (initial) {
                        initial = false
                        return
                    }
                    prefs
                        .getString("${transportTypes[position]}_endpoint", null)
                        ?.let { endpoint.setText(it) }
                }
            }

        panel = section("МАРШРУТИЗАЦИЯ")
        mode =
            choice(
                panel,
                "Маршрутизация",
                arrayOf(
                    "All apps",
                    "Only selected apps",
                    "Exclude selected apps",
                    "Network routing",
                ),
                prefs.getInt("mode", 0),
            )
        apps = VpnProfiles.apps(this).toMutableSet()
        useGlobalApps =
            CheckBox(this).apply {
                text = "Общий список приложений"
                textSize = 15f
                setTextColor(ink)
                buttonTintList = android.content.res.ColorStateList.valueOf(accent)
                isChecked = prefs.getBoolean("global_apps", false)
            }
        panel.addView(useGlobalApps)
        appsSummary = label(panel, "")
        updateAppsSummary()
        useGlobalApps.setOnCheckedChangeListener { _, global ->
            apps =
                (if (global) VpnProfiles.globalApps(this)
                    else prefs.getStringSet("apps", emptySet())!!)
                    .toMutableSet()
            updateAppsSummary()
        }
        button(panel, "Выбрать приложения") { chooseApps() }
        routes = field(panel, "Подсети · IPv4 CIDR, по одной на строку", "routes")
        dns = field(panel, "DNS · IPv4", "dns", "1.1.1.1")
        panel.addView(android.widget.Switch(this).apply {
            text="Системный DNS физической сети для всего VPN"
            isChecked=VpnProfiles.dnsMode(this@VpnActivity)=="system"
            setOnCheckedChangeListener { _,checked -> VpnProfiles.setDnsMode(this@VpnActivity,if(checked) "system" else "tunnel") }
        })
        label(
            panel,
            "DNS через выбранный VPN-выход блокируется при отказе. Системный DNS работает вне VPN. Совпадение локальных и удалённых подсетей не обрабатывается.",
        )
        panel = section("СЕРТИФИКАТ И БЕЗОПАСНОСТЬ")
        identity =
            label(
                panel,
                try {
                    VpnIdentity.load(this).let { keys -> listOfNotNull(if(keys.has("certificate")) "Клиентский сертификат установлен" else null, if(keys.has("awg_config")) "Ключи AmneziaWG импортированы" else null).joinToString("\n") }
                } catch (_: Exception) {
                    "Клиентский сертификат не импортирован"
                },
            )
        val advanced =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                visibility = android.view.View.GONE
            }
        button(panel, "Дополнительные настройки") {
            advanced.visibility =
                if (advanced.visibility == android.view.View.GONE) android.view.View.VISIBLE
                else android.view.View.GONE
        }
        panel.addView(advanced)
        button(advanced, "Импортировать сертификат .p12") {
            require(!prefs.getBoolean("managed_profile",false)) {"Сертификат управляемого профиля задаёт сервер; создайте отдельный профиль"}
            require(prefs.getString("transport", "quic") != "awg") { "Для сертификата создайте отдельный профиль QUIC/HTTPS" }
            startActivityForResult(
                Intent(Intent.ACTION_OPEN_DOCUMENT)
                    .setType("*/*")
                    .addCategory(Intent.CATEGORY_OPENABLE),
                11,
            )
        }
        ca = field(advanced, "CA сервера PEM (пусто для публичного сертификата)", "ca")
        if(prefs.getBoolean("managed_profile",false)) {
            label(panel,"Адреса, DNS и сертификат задаёт сервер. Используйте «Обновить конфиг». Маршруты, приложения и экономия остаются вашими.")
            endpoint.isEnabled=false;hostname.isEnabled=false;dns.isEnabled=false;ca.isEnabled=false
        }
        panel = section("УПРАВЛЕНИЕ")
        button(panel, "Сохранить настройки") {
            save()
            finish()
        }
        button(panel, "Запустить VPN") {
            if(VpnProfiles.multiple(this)) MultipleVpnPlan.load(this) else save()
            val request = VpnService.prepare(this)
            if (request != null) startActivityForResult(request, 12) else startVPN()
        }
        button(panel, "Остановить VPN") {
            startService(Intent(this, LabVpnService::class.java).setAction("stop"))
        }
        label(
            panel,
            "Смена транспорта или маршрутов: остановите VPN, измените настройки и запустите снова. Текущие соединения завершатся.",
        )
        val diagnostics = section("ДИАГНОСТИКА")
        label(diagnostics, "RTT для всех VPN-профилей. Изменения применяются сразу. Echo использует собственные измерения.")
        val rttPrefs = VpnRttSettings.preferences(this)
        for ((key, title, default) in listOf(Triple("screen_on", "Экран включён · RTT", 1000L), Triple("screen_off", "Экран выключен · RTT", 0L))) {
            val spinner = choice(diagnostics, title, VpnRttSettings.labels,
                VpnRttSettings.values.indexOf(rttPrefs.getLong(key, default)).coerceAtLeast(0))
            spinner.onItemSelectedListener = object : AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent: AdapterView<*>?) {}
                override fun onItemSelected(parent: AdapterView<*>?, view: android.view.View?, position: Int, id: Long) {
                    rttPrefs.edit().putLong(key, VpnRttSettings.values[position]).apply()
                }
            }
        }
        label(diagnostics, "Настройка управляет RTT шлюза и транзита. При выключенных измерениях остаётся служебная проверка связи раз в 5 секунд; она не отображается на графике. Keep-alive и переключение сетей сохраняются.")
        val maximum = section("ОБЩАЯ СЕССИЯ И КАРУСЕЛЬ")
        val demux = android.widget.Switch(this).apply {
            text="Общая сессия QUIC / HTTPS"
            isChecked=prefs.getBoolean("demux_enabled",prefs.getBoolean("max_availability",false))
            isEnabled=!LabVpnService.active && selectedTransport()!="awg"
            setOnCheckedChangeListener { _, value -> prefs.edit().putBoolean("demux_enabled",value).apply() }
        }
        maximum.addView(demux)
        maximum.addView(android.widget.Switch(this).apply {
            text="Максимальная доступность · готовить LTE"
            isChecked=prefs.getBoolean("max_availability",false)
            isEnabled=!LabVpnService.active && selectedTransport()!="awg"
            setOnCheckedChangeListener { _, value ->
                if(value) demux.isChecked=true
                prefs.edit().putBoolean("max_availability",value).putBoolean("demux_enabled",demux.isChecked).apply()
            }
        })
        label(maximum,"Общая сессия сохраняет TCP и UDP при смене QUIC/HTTPS одного сервера. В экономии рабочий Wi-Fi не готовит LTE. Максимальная доступность расходует больше LTE и батареи; готовый резерв требует пула от 2 соединений. Изменения применяются после перезапуска выхода. AWG работает самостоятельно.")
        for(kind in transportTypes.filter{it in listOf("quic","https")}) {
            val modes=listOf("auto","reserve","disabled")
            val profileMode=choice(maximum,"${kind.uppercase()} · участие",arrayOf("Автоматически","Только резерв","Не использовать"),modes.indexOf(prefs.getString("${kind}_mode","auto")).coerceAtLeast(0))
            profileMode.isEnabled=!LabVpnService.active
            profileMode.onItemSelectedListener=object:AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent:AdapterView<*>?){}
                override fun onItemSelected(parent:AdapterView<*>?,view:android.view.View?,position:Int,id:Long){prefs.edit().putString("${kind}_mode",modes[position]).apply()}
            }
            val defaultPool=if(kind==selectedTransport() && prefs.getBoolean("max_availability",false)) 2 else 1
            val pool=choice(maximum,"${kind.uppercase()} · соединений всего",arrayOf("1 · карусель выключена","2","3","4","5"),(prefs.getInt("${kind}_pool_size",defaultPool)-1).coerceIn(0,4))
            pool.isEnabled=!LabVpnService.active
            pool.onItemSelectedListener=object:AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent:AdapterView<*>?){}
                override fun onItemSelected(parent:AdapterView<*>?,view:android.view.View?,position:Int,id:Long){prefs.edit().putInt("${kind}_pool_size",position+1).apply()}
            }
            maximum.addView(android.widget.CheckBox(this).apply {
                text="${kind.uppercase()} · редко проверять профиль «Только резерв»"
                isChecked=prefs.getBoolean("${kind}_check_reserve",false);isEnabled=!LabVpnService.active
                setOnCheckedChangeListener{_,value->prefs.edit().putBoolean("${kind}_check_reserve",value).apply()}
            })
        }
        label(maximum,"Пул общий для Wi-Fi и LTE, а не отдельный для каждой сети. Выбранный выше транспорт имеет первый приоритет. «Только резерв» включается после отказа всех автоматических путей; редкие проверки не запускают его карусель. Разные сохранённые серверы остаются разными выходами.")
        fun budget(title:String,key:String,default:Long,limit:Long, target: android.content.SharedPreferences = prefs, container: LinearLayout = maximum) {
            label(container,title)
            container.addView(EditText(this).apply {
                inputType=android.text.InputType.TYPE_CLASS_NUMBER
                setText(target.getLong(key,default).toString())
                addTextChangedListener(object: android.text.TextWatcher {
                    override fun beforeTextChanged(s:CharSequence?,start:Int,count:Int,after:Int) {}
                    override fun onTextChanged(s:CharSequence?,start:Int,before:Int,count:Int) { s?.toString()?.toLongOrNull()?.let { if(it in 0..limit) target.edit().putLong(key,it).apply() } }
                    override fun afterTextChanged(s:android.text.Editable?) {}
                })
            })
        }
        budget("Бюджет упреждающих копий, KiB/мин · 0 — только обычные повторы", "bond_copy_kib",256,65536)
        val sharedBudget = section("ОБЩИЙ ЛИМИТ LTE")
        budget("На один запуск VPN, MiB · 0 без ограничения", "cell_mib",0,1048576,VpnBudgetSettings.preferences(this),sharedBudget)
        label(sharedBudget,"Общий для всех профилей и транспортов. Применяется после остановки и запуска всего VPN; переподключение отдельного выхода лимит не сбрасывает. После исчерпания остаётся Wi-Fi. Учитываются внешние данные сокетов, включая шифрование, но не IP/TCP-заголовки и повторы TCP ядром. Уже летящие входящие данные могут превысить порог; это не счётчик оператора.")
        label(maximum,"Бюджет копий делится поровну между направлениями. После его исчерпания обычное восстановление продолжается. Сторонние VPN и multiple пока не поддерживают максимальную доступность.")
        val reserve = section("РЕЗЕРВНАЯ СЕТЬ VPN")
        label(reserve, "Настройки фонового резерва общие для всех профилей и применяются сразу. При работе через LTE клиент проверяет появившийся Wi-Fi, включая лимитный, и переходит на него после подтверждения доступности. Эти проверки расходуют трафик Wi-Fi. RTT текущего VPN настраивается отдельно.")
        val reservePrefs = VpnReserveSettings.preferences(this)
        for ((key, title, default) in listOf(
            Triple("wifi_on", "Фоновый резерв Wi-Fi · экран включён", true),
            Triple("wifi_off", "Фоновый резерв Wi-Fi · экран выключен", true),
            Triple("cell_on", "Wi-Fi → LTE · экран включён", false),
            Triple("cell_off", "Wi-Fi → LTE · экран выключен", false),
            Triple("metered_wifi", "Разрешить фоновые проверки лимитного Wi-Fi", false))) {
            reserve.addView(android.widget.Switch(this).apply {
                text = title
                isChecked = reservePrefs.getBoolean(key, default)
                setOnCheckedChangeListener { _, checked -> reservePrefs.edit().putBoolean(key, checked).apply() }
            })
        }
        label(reserve, "Выключенная проверка LTE не удерживает мобильную сеть для резерва. При потере основной сети VPN всё равно может перейти на LTE и расходовать мобильный трафик. Echo — отдельный активный эксперимент с проверками обеих сетей.")
        label(reserve, "QUIC/HTTPS проверяют VPN-сервер через резерв. Для AWG используется проверка интернета Android: второй AWG-туннель с тем же ключом нарушил бы текущий канал. Доступность AWG-сервера подтверждается после переключения. Captive portal не считается готовым Wi-Fi.")
        label(diagnostics, "IPv4: QUIC, HTTPS и AmneziaWG поддерживают TCP и UDP. IPv6 заблокирован для захваченного VPN трафика.")
        logs = label(diagnostics, "")
        logs.textSize = 12f
        handler.post(
            object : Runnable {
                override fun run() {
                    state.text =
                        if (LabVpnService.active)
                            if(LabVpnService.multipleMode) MultipleVpnState.summary() else "${LabVpnService.status} · RTT ${LabVpnService.rttLabel(android.os.SystemClock.elapsedRealtime())}"
                        else LabVpnService.status
                    renderDashboard()
                    logs.text = LabVpnService.log()
                    handler.postDelayed(this, 500)
                }
            }
        )
    }

    private fun renderDashboard() {
        val now = android.os.SystemClock.elapsedRealtime()
        dashboardBudget.text = VpnDashboardText.budget(VpnDashboardEvents.budget())
        startControl.isEnabled = !LabVpnService.active
        stopControl.isEnabled = LabVpnService.active
        val run = VpnDashboardEvents.runId
        if (dashboardRunId != run) { profileLabelsByExit.clear(); dashboardRunId = run }
        val cards = VpnDashboardEvents.dashboards()
        val names = VpnDashboardEvents.names()
        val ids = cards.map { it.exitId }.toSet()
        exitViews.keys.filter { it !in ids }.toList().forEach { dashboardExits.removeView(exitViews.remove(it)) }
        if (cards.isEmpty()) {
            if (!exitViews.containsKey("")) exitViews[""] = label(dashboardExits, "VPN выключен · выберите профиль и запустите VPN")
            return
        }
        exitViews.remove("")?.let { dashboardExits.removeView(it) }
        val permission = checkSelfPermission(android.Manifest.permission.ACCESS_FINE_LOCATION) == android.content.pm.PackageManager.PERMISSION_GRANTED
        val location = getSystemService(android.location.LocationManager::class.java).isLocationEnabled
        val interval = VpnRttSettings.interval(this)
        for (card in cards) {
            val path = card.paths.firstOrNull { it.pathId == card.activePathId }
            val radio = if (!permission || !location) "Недоступно" else when (VpnDashboardText.network(path?.network.orEmpty())) {
                "Wi-Fi" -> wifiRadio
                "LTE" -> cellRadio
                else -> "Недоступно"
            }
            val view = exitViews.getOrPut(card.exitId) { label(dashboardExits, "").apply { textSize = 15f; setTextColor(ink) } }
            if (profileLabelsByExit[card.exitId]?.first != card.generation) {
                val kind = VpnProfiles.preferences(this, card.exitId).getString("transport", "quic").orEmpty()
                val selected = transportName(kind)
                profileLabelsByExit[card.exitId] = card.generation to mapOf(
                    "" to selected, card.exitId to selected,
                    "${card.exitId}.quic" to "QUIC", "${card.exitId}.https" to "HTTPS / WebSocket",
                    "${card.exitId}.awg" to "AmneziaWG")
            }
            view.text = VpnDashboardText.card(card, names[card.exitId] ?: card.exitId, now, interval, radio,
                profileLabelsByExit.getValue(card.exitId).second)
        }
    }

    override fun onResume() {
        super.onResume()
        radios = RadioMonitor(this, { wifi, cell ->
            wifiRadio = wifi.lineSequence().filter { it.startsWith("Wi-Fi:") || it.startsWith("Сигнал") }.joinToString(" · ")
            cellRadio = cell.lineSequence().take(3).joinToString(" · ")
        }, { _, _ -> }).also { it.start() }
    }

    override fun onPause() {
        radios?.close(); radios = null
        wifiRadio = "Недоступно"; cellRadio = "Недоступно"
        super.onPause()
    }

    private fun updateProfile(id:String,allowOverBudget:Boolean=false) {
        val progress=AlertDialog.Builder(this).setMessage("Проверяем конфигурацию…").setCancelable(false).show()
        Thread({
            try {
                val incoming=ProfileUpdate.fetch(this,id,allowOverBudget)
                val identity=VpnIdentity.load(this,id)
                val current=identity.optJSONObject("update_envelope") ?: org.json.JSONObject().put("server_config",identity.optJSONObject("server_config") ?: org.json.JSONObject())
                val changes=ProfileUpdate.diff(current,incoming)
                val selected=VpnProfiles.preferences(this,id).getString("transport","quic").orEmpty()
                val available=ProfileImport.transports(incoming.getJSONObject("server_config"))
                val transportChange=if(selected !in available) "\nВыбранный ${transportName(selected)} удалён; после применения используется ${transportName(available.first())}." else ""
                handler.post {
                    progress.dismiss()
                    val dialog=AlertDialog.Builder(this).setTitle(if(ProfileUpdate.compatible(incoming)) "Изменения конфигурации" else "Требуется обновить приложение")
                        .setMessage((if(changes.length()==0) "Настройки подключения не изменились." else changes.toString(2))+
                            transportChange+"\n\nЛокальные маршруты, приложения и экономия сохранятся. Переподключится только этот выход.")
                        .setNegativeButton("Отмена",null)
                    if(ProfileUpdate.compatible(incoming)) dialog.setPositiveButton("Применить и переподключить") {_,_->
                        try {ProfileUpdate.apply(this,id,incoming);recreate()}catch(e:Exception){error(e)}
                    }
                    val caps=incoming.getJSONObject("capabilities")
                    if(caps.optString("apk_url").isNotEmpty() && caps.optString("apk_sha256").isNotEmpty())
                        dialog.setNeutralButton("Загрузить APK") {_,_->downloadUpdateApk(id,incoming)}
                    dialog.show()
                }
            } catch(e:Exception) {
                handler.post {
                    progress.dismiss()
                    if(e is LabVpnService.UpdateConsentRequired) transferConsent {updateProfile(id,true)} else error(e)
                }
            }
        },"profile-update").start()
    }
    private fun transferConsent(retry:()->Unit) {
        AlertDialog.Builder(this).setTitle("Служебная загрузка через LTE")
            .setMessage("Общий LTE-бюджет исчерпан. Разрешить только эту загрузку сверх лимита? Трафик будет учтён; VPN-бюджет не сбросится.")
            .setNegativeButton("Отмена",null).setPositiveButton("Разрешить загрузку") {_,_->retry()}.show()
    }
    private fun downloadUpdateApk(id:String,envelope:org.json.JSONObject,allowOverBudget:Boolean=false) {
        val progress=AlertDialog.Builder(this).setMessage("Загружаем и проверяем APK…").setCancelable(false).show()
        Thread({
            try {
                val file=ProfileUpdate.downloadApk(this,id,envelope,allowOverBudget)
                val hash=envelope.getJSONObject("capabilities").getString("apk_sha256")
                handler.post {
                    progress.dismiss()
                    AlertDialog.Builder(this).setTitle("APK проверен")
                        .setMessage("Hash, пакет и подпись совпадают. Открыть системный установщик?")
                        .setNegativeButton("Отмена") {_,_->file.delete()}
                        .setPositiveButton("Открыть установщик") {_,_->
                            try {
                                if(android.os.Build.VERSION.SDK_INT>=26 && !packageManager.canRequestPackageInstalls()) {
                                    startActivity(Intent(android.provider.Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES,android.net.Uri.parse("package:$packageName")))
                                    Toast.makeText(this,"Разрешите установку и повторите загрузку APK",Toast.LENGTH_LONG).show()
                                    file.delete()
                                } else startActivity(ProfileUpdate.installerIntent(this,file,hash))
                            }catch(e:Exception){file.delete();error(e)}
                        }.show()
                }
            }catch(e:Exception){handler.post {progress.dismiss();if(e is LabVpnService.UpdateConsentRequired) transferConsent {downloadUpdateApk(id,envelope,true)} else error(e)}}
        },"apk-update").start()
    }
    private fun save() {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        require(endpoint.text.contains(':')) { "Укажите домен:порт" }
        if (selectedTransport() != "awg") require(hostname.text.isNotBlank()) { "Укажите TLS hostname" }
        if (mode.selectedItemPosition == 1)
            require(apps.isNotEmpty()) { "Выберите хотя бы одно приложение" }
        if (mode.selectedItemPosition == 3) VpnRoutes.parse(routes.text.toString())
        else VpnRoutes.parse(dns.text.toString() + "/32")
        val keys = VpnIdentity.load(this)
        require(if (selectedTransport() == "awg") keys.has("awg_config") else keys.has("certificate")) { "Для другого протокола импортируйте отдельный профиль" }
        prefs
            .edit()
            .putString("transport", selectedTransport())
            .putString("endpoint", endpoint.text.toString().trim())
            .putString("hostname", hostname.text.toString().trim())
            .putInt("mode", mode.selectedItemPosition)
            .putBoolean("global_apps", useGlobalApps.isChecked)
            .putStringSet(
                "apps",
                if (useGlobalApps.isChecked) prefs.getStringSet("apps", emptySet()) else apps,
            )
            .putString("routes", routes.text.toString())
            .putString("dns", dns.text.toString().trim())
            .putString("ca", ca.text.toString())
            .apply()
    }

    private fun startVPN() {
        startForegroundService(Intent(this, LabVpnService::class.java))
    }

    private fun deleteProfile() {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        AlertDialog.Builder(this)
            .setTitle("Удалить ${VpnProfiles.current(this).name}?")
            .setMessage(
                "Настройки и сертификат этого профиля будут удалены с телефона. Общий список приложений сохранится."
            )
            .setNegativeButton("Отмена", null)
            .setPositiveButton("Удалить") { _, _ ->
                try {
                    VpnProfiles.delete(this)
                    recreate()
                } catch (e: Exception) {
                    error(e)
                }
            }
            .show()
    }

    private fun updateAppsSummary() {
        appsSummary.text =
            "${if(useGlobalApps.isChecked) "Общий список" else "Список профиля"}: ${apps.size} приложений"
    }

    private fun profileNameDialog(rename: Boolean) {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        val name =
            EditText(this).apply {
                setSingleLine()
                if (rename) setText(VpnProfiles.current(this@VpnActivity).name)
            }
        AlertDialog.Builder(this)
            .setTitle(if (rename) "Название профиля" else "Новый профиль")
            .setView(name)
            .setNegativeButton("Отмена", null)
            .setPositiveButton("Сохранить") { _, _ ->
                try {
                    if (rename) VpnProfiles.rename(this, name.text.toString())
                    else VpnProfiles.create(this, name.text.toString())
                    recreate()
                } catch (e: Exception) {
                    error(e)
                }
            }
            .show()
    }

    private fun chooseApps() {
        startActivityForResult(
            Intent(this, AppSelectionActivity::class.java)
                .putStringArrayListExtra("apps", ArrayList(apps))
                .putExtra(
                    "title",
                    if (useGlobalApps.isChecked) "Общие приложения"
                    else "Приложения · ${VpnProfiles.current(this).name}",
                ),
            13,
        )
    }

    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (resultCode != RESULT_OK) return
        if (requestCode == 14) {
            try {
                val uri = data?.data ?: return
                val raw = contentResolver.openInputStream(uri)!!.use { input ->
                    val bytes = ByteArray(32769)
                    var count = 0
                    while (count < bytes.size) { val n = input.read(bytes, count, bytes.size - count); if (n < 0) break; count += n }
                    require(count <= 32768) { "Конфиг слишком большой" }
                    String(bytes, 0, count, Charsets.UTF_8).also { bytes.fill(0) }
                }
                if (raw.trimStart().startsWith("{")) {
                    val profile = org.json.JSONObject(raw)
                    require(ProfileImport.validate(profile) == "vpn") { "Нужен VPN профиль" }
                    AlertDialog.Builder(this).setTitle("Импорт VPN профиля")
                        .setMessage("${profile.optString("name")} · ${profile.getString("hostname")}\n${ProfileImport.transports(profile).joinToString(" / ") { transportName(it) }}")
                        .setPositiveButton("Импортировать") { _, _ -> try { ProfileImport.save(this, profile); recreate() } catch(e: Exception) { error(e) } }
                        .setNegativeButton("Отмена", null).show()
                } else AwgImport.review(this, raw, { recreate() })
            } catch (e: Exception) { error(e) }
            return
        }
        if (requestCode == 13) {
            apps = (data?.getStringArrayListExtra("apps") ?: return).toMutableSet()
            if (useGlobalApps.isChecked) VpnProfiles.setGlobalApps(this, apps)
            else prefs.edit().putStringSet("apps", apps).apply()
            updateAppsSummary()
            return
        }
        if (requestCode == ProfileImport.REQUEST) {
            recreate()
            return
        }
        if (requestCode == 12) {
            startVPN()
            return
        }
        if (requestCode == 11) {
            val uri = data?.data ?: return
            val password =
                EditText(this).apply {
                    inputType = InputType.TYPE_CLASS_TEXT or InputType.TYPE_TEXT_VARIATION_PASSWORD
                }
            AlertDialog.Builder(this)
                .setTitle("Пароль PKCS#12")
                .setView(password)
                .setPositiveButton("Импортировать") { _, _ ->
                    try {
                        val bytes =
                            contentResolver.openInputStream(uri)!!.use { input ->
                                val out = java.io.ByteArrayOutputStream()
                                val chunk = ByteArray(8192)
                                while (true) {
                                    val n = input.read(chunk)
                                    if (n < 0) break
                                    require(out.size() + n <= 1024 * 1024) {
                                        "Файл слишком большой"
                                    }
                                    out.write(chunk, 0, n)
                                }
                                out.toByteArray()
                            }
                        identity.text =
                            VpnIdentity.import(this, bytes, password.text.toString().toCharArray())
                        bytes.fill(0)
                        password.text.clear()
                    } catch (e: Exception) {
                        error(e)
                    }
                }
                .setNegativeButton("Отмена", null)
                .show()
        }
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        super.onDestroy()
    }
}
