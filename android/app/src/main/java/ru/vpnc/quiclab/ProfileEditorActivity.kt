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

class ProfileEditorActivity : Activity() {
    private val accent = Color.rgb(0, 128, 117)
    private val ink = Color.rgb(28, 28, 30)
    private val secondary = Color.rgb(112, 112, 117)

    private fun dp(n: Int) = (n * resources.displayMetrics.density).toInt()

    private fun rounded(color: Int) =
        android.graphics.drawable.GradientDrawable().apply {
            setColor(color)
            cornerRadius = dp(14).toFloat()
        }

    private val handler = Handler(Looper.getMainLooper())
    private var pendingIdentity: org.json.JSONObject? = null
    private lateinit var transport: Spinner
    private lateinit var mode: Spinner
    private lateinit var endpoint: EditText
    private lateinit var hostname: EditText
    private lateinit var routes: EditText
    private lateinit var dns: EditText
    private lateinit var ca: EditText
    private lateinit var copyBudget: EditText
    private lateinit var demuxSwitch: android.widget.Switch
    private lateinit var maximumSwitch: android.widget.Switch
    private lateinit var identity: TextView
    private lateinit var useGlobalApps: CheckBox
    private lateinit var appsSummary: TextView
    private var apps = mutableSetOf<String>()
    private lateinit var prefs: PreferenceDraft
    private lateinit var profileId: String
    private var initialForm = ""
    private var initialApps = emptySet<String>()
    private var draftGlobalApps = emptySet<String>()
    private val sectionViews = linkedMapOf<String, List<android.view.View>>()
    private var category = "Подключение"
    private var busy = false
    private var initialising = true
    private val transportTypes get() = run {
        val selected = prefs.getString("transport", "quic").orEmpty()
        val fallback = if (selected in setOf("awg", "vless")) setOf(selected) else setOf("quic", "https")
        val available = prefs.getStringSet("available_transports", fallback) ?: fallback
        listOf("quic", "https", "awg", "vless").filter { it in available }.ifEmpty { fallback.toList() }
    }
    private fun selectedTransport() = transportTypes[transport.selectedItemPosition]
    private fun transportName(value: String) = when(value) { "quic" -> "QUIC"; "https" -> "HTTPS / WebSocket"; "awg" -> "AmneziaWG"; "vless" -> "VLESS"; else -> value.uppercase() }

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
            .setTitle("Не удалось выполнить действие")
            .setMessage(e.message ?: e.toString())
            .setPositiveButton("OK", null)
            .show()
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        profileId = intent.getStringExtra("profile_id") ?: VpnProfiles.current(this).id
        prefs = PreferenceDraft(VpnProfiles.preferences(this,profileId))
        draftGlobalApps = VpnProfiles.globalApps(this)
        pendingIdentity = lastNonConfigurationInstance as? org.json.JSONObject
        buildEditor()
        if(savedInstanceState==null && intent.getBooleanExtra("update_profile",false)){
            intent.removeExtra("update_profile")
            if(prefs.getBoolean("managed_profile",false))handler.post{updateProfile(profileId)}
        }
    }

    private fun buildEditor() {
        initialising = true
        sectionViews.clear()
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
                setOnClickListener { leaveEditor() }
            },
            LinearLayout.LayoutParams(dp(48), dp(56)),
        )
        toolbar.addView(
            TextView(this).apply {
                text = VpnProfiles.list(this@ProfileEditorActivity).first { it.id == profileId }.name
                gravity = android.view.Gravity.CENTER_VERTICAL
                textSize = 21f
                setTextColor(ink)
                setTypeface(null, android.graphics.Typeface.BOLD)
            },
            LinearLayout.LayoutParams(0, dp(56), 1f).apply {
                gravity = android.view.Gravity.CENTER_VERTICAL
            },
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
        val categories = LinearLayout(this)
        outer.addView(categories,1)
        for (name in listOf("Подключение","Маршруты","Устойчивость","Ещё")) {
            categories.addView(Button(this).apply { text=name;isAllCaps=false;textSize=10f;isSingleLine=true;setPadding(dp(2),0,dp(2),0);setOnClickListener {category=name;showCategory()} },LinearLayout.LayoutParams(0,dp(52),1f))
        }
        fun section(title: String): LinearLayout {
            val first = root.childCount
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
                sectionViews[title] = listOf(root.getChildAt(first),this)
            }
        }
        var panel = section("ПОДКЛЮЧЕНИЕ")
        if(prefs.getBoolean("managed_profile",false)) button(panel,"Обновить конфигурацию с сервера") { leaveDraft { updateProfile(profileId) } }
        transport =
            choice(
                panel,
                "Транспорт",
                transportTypes.map { transportName(it) }.toTypedArray(),
                transportTypes.indexOf(prefs.getString("transport", "quic")).coerceAtLeast(0),
            )
        endpoint = field(panel, "Сервер · домен:порт", "endpoint")
        hostname = field(panel, "Имя сервера в сертификате TLS", "hostname")
        hostname.isEnabled = selectedTransport() !in setOf("awg", "vless")
        label(panel, "AmneziaWG: импорт .conf или QR. RTT — ICMP ping endpoint через туннель; частота задаётся в общих настройках. VLESS: отдельный импорт URI / JSON; TLS/REALITY и SNI задаются при импорте. Доступные протоколы задаются при выдаче профиля.")
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
                    handler.post { updateTransportControls() }
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
                    "Все приложения",
                    "Выбранные приложения",
                    "Кроме выбранных приложений",
                    "Подсети IPv4",
                ),
                prefs.getInt("mode", 0),
            )
        apps = (if(prefs.getBoolean("global_apps",false)) draftGlobalApps else prefs.getStringSet("apps",emptySet())!!).toMutableSet()
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
                (if (global) draftGlobalApps
                    else prefs.getStringSet("apps", emptySet())!!)
                    .toMutableSet()
            updateAppsSummary()
        }
        button(panel, "Выбрать приложения") { chooseApps() }
        routes = field(panel, "Подсети · IPv4 CIDR, по одной на строку", "routes")
        dns = field(panel, "DNS · IPv4", "dns", "1.1.1.1")
        label(
            panel,
            "DNS через выбранный VPN-выход блокируется при отказе. Системный DNS работает вне VPN. Совпадение локальных и удалённых подсетей не обрабатывается.",
        )
        panel = section("СЕРТИФИКАТ И БЕЗОПАСНОСТЬ")
        identity =
            label(
                panel,
                try {
                    VpnIdentity.load(this,profileId).let { keys -> listOfNotNull(if(keys.has("certificate")) "Клиентский сертификат установлен" else null, if(keys.has("awg_config")) "Ключи AmneziaWG импортированы" else null, if(keys.has("vless_config")) "VLESS импортирован · данные доступа зашифрованы" else null).joinToString("\n") }
                } catch (_: Exception) {
                    "Данные доступа не импортированы"
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
            require(prefs.getString("transport", "quic") !in setOf("awg", "vless")) { "Для сертификата создайте отдельный профиль QUIC/HTTPS" }
            startActivityForResult(
                Intent(Intent.ACTION_OPEN_DOCUMENT)
                    .setType("*/*")
                    .addCategory(Intent.CATEGORY_OPENABLE),
                11,
            )
        }
        ca = field(advanced, "CA сервера PEM (пусто для публичного сертификата)", "ca")
        if(selectedTransport() == "vless") {
            endpoint.isEnabled = false; hostname.isEnabled = false; ca.isEnabled = false
            label(panel, "VLESS: TLS/REALITY и SNI сохранены при импорте. Для изменения сервера импортируйте отдельный профиль.")
        }
        if(prefs.getBoolean("managed_profile",false)) {
            label(panel,"Адреса, DNS и сертификат задаёт сервер. Используйте «Обновить конфиг». Маршруты, приложения и экономия остаются вашими.")
            endpoint.isEnabled=false;hostname.isEnabled=false;dns.isEnabled=false;ca.isEnabled=false
        }
        val maximum = section("ОБЩАЯ СЕССИЯ И КАРУСЕЛЬ")
        val demux = android.widget.Switch(this).apply {
            text="Общая сессия QUIC / HTTPS"
            isChecked=prefs.getBoolean("demux_enabled",prefs.getBoolean("max_availability",false))
            isEnabled=selectedTransport() !in setOf("awg", "vless")
            setOnCheckedChangeListener { _, value -> prefs.edit().putBoolean("demux_enabled",value).apply() }
        }
        demuxSwitch=demux
        maximum.addView(demux)
        maximumSwitch=android.widget.Switch(this).apply {
            text="Максимальная доступность · готовить LTE"
            isChecked=prefs.getBoolean("max_availability",false)
            isEnabled=selectedTransport() !in setOf("awg", "vless")
            setOnCheckedChangeListener { _, value ->
                if(value) demux.isChecked=true
                prefs.edit().putBoolean("max_availability",value).putBoolean("demux_enabled",demux.isChecked).apply()
            }
        }
        maximum.addView(maximumSwitch)
        label(maximum,"Общая сессия сохраняет TCP и UDP при смене QUIC/HTTPS одного сервера. В экономии рабочий Wi-Fi не готовит LTE. Максимальная доступность расходует больше LTE и батареи; готовый резерв требует пула от 2 соединений. Изменения применяются после перезапуска выхода. AWG и VLESS работают самостоятельно; смена сети может прервать соединения.")
        for(kind in transportTypes.filter{it in listOf("quic","https")}) {
            val modes=listOf("auto","reserve","disabled")
            val profileMode=choice(maximum,"${kind.uppercase()} · участие",arrayOf("Автоматически","Только резерв","Не использовать"),modes.indexOf(prefs.getString("${kind}_mode","auto")).coerceAtLeast(0))
            profileMode.isEnabled=true
            profileMode.onItemSelectedListener=object:AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent:AdapterView<*>?){}
                override fun onItemSelected(parent:AdapterView<*>?,view:android.view.View?,position:Int,id:Long){prefs.edit().putString("${kind}_mode",modes[position]).apply()}
            }
            val defaultPool=if(kind==selectedTransport() && prefs.getBoolean("max_availability",false)) 2 else 1
            val pool=choice(maximum,"${kind.uppercase()} · соединений всего",arrayOf("1 · карусель выключена","2","3","4","5"),(prefs.getInt("${kind}_pool_size",defaultPool)-1).coerceIn(0,4))
            pool.isEnabled=true
            pool.onItemSelectedListener=object:AdapterView.OnItemSelectedListener {
                override fun onNothingSelected(parent:AdapterView<*>?){}
                override fun onItemSelected(parent:AdapterView<*>?,view:android.view.View?,position:Int,id:Long){prefs.edit().putInt("${kind}_pool_size",position+1).apply()}
            }
            maximum.addView(android.widget.CheckBox(this).apply {
                text="${kind.uppercase()} · редко проверять профиль «Только резерв»"
                isChecked=prefs.getBoolean("${kind}_check_reserve",false);isEnabled=true
                setOnCheckedChangeListener{_,value->prefs.edit().putBoolean("${kind}_check_reserve",value).apply()}
            })
        }
        label(maximum,"Пул общий для Wi-Fi и LTE, а не отдельный для каждой сети. Выбранный выше транспорт имеет первый приоритет. «Только резерв» включается после отказа всех автоматических путей; редкие проверки не запускают его карусель. Разные сохранённые серверы остаются разными выходами.")
        fun budget(title:String,key:String,default:Long,limit:Long, target: android.content.SharedPreferences = prefs, container: LinearLayout = maximum) {
            label(container,title)
            copyBudget=EditText(this).apply {
                inputType=android.text.InputType.TYPE_CLASS_NUMBER
                setText(target.getLong(key,default).toString())
                addTextChangedListener(object: android.text.TextWatcher {
                    override fun beforeTextChanged(s:CharSequence?,start:Int,count:Int,after:Int) {}
                    override fun onTextChanged(s:CharSequence?,start:Int,before:Int,count:Int) { s?.toString()?.toLongOrNull()?.let { if(it in 0..limit) target.edit().putLong(key,it).apply() } }
                    override fun afterTextChanged(s:android.text.Editable?) {}
                })
            }
            container.addView(copyBudget)
        }
        budget("Бюджет упреждающих копий, KiB/мин · 0 — только обычные повторы", "bond_copy_kib",256,65536)
        label(maximum,"Бюджет копий делится поровну между направлениями. После его исчерпания обычное восстановление продолжается. Сторонние VPN и multiple пока не поддерживают максимальную доступность.")
        val footer=LinearLayout(this)
        button(footer,"Сохранить") { saveEditor() }
        outer.addView(footer)
        EditorViewState.assign(outer)
        showCategory()
        initialForm=formFingerprint();initialApps=apps.toSet()
        handler.post { prefs.acceptInitialState(); initialising=false }
    }

    override fun onRetainNonConfigurationInstance(): Any? = pendingIdentity

    override fun onSaveInstanceState(out: Bundle) {
        out.putSerializable("draft", prefs.snapshot())
        out.putStringArrayList("apps", ArrayList(apps))
        out.putStringArrayList("globalApps", ArrayList(draftGlobalApps))
        out.putString("category", category)
        super.onSaveInstanceState(out)
    }
    @Suppress("DEPRECATION", "UNCHECKED_CAST")
    override fun onRestoreInstanceState(saved: Bundle) {
        // Run after initial spinner callbacks establish the unchanged baseline.
        handler.post {
            super.onRestoreInstanceState(saved)
            (saved.getSerializable("draft") as? Map<String, *>)?.let { prefs.restore(it) }
            draftGlobalApps = saved.getStringArrayList("globalApps")?.toSet() ?: draftGlobalApps
            apps = saved.getStringArrayList("apps")?.toMutableSet() ?: apps
            category = saved.getString("category") ?: category
            updateAppsSummary(); showCategory()
        }
    }

    private fun showCategory() {
        val visible=when(category) {"Подключение"->setOf("ПОДКЛЮЧЕНИЕ");"Маршруты"->setOf("МАРШРУТИЗАЦИЯ");"Устойчивость"->setOf("ОБЩАЯ СЕССИЯ И КАРУСЕЛЬ");else->setOf("СЕРТИФИКАТ И БЕЗОПАСНОСТЬ")}
        sectionViews.forEach{(name,views)->views.forEach{it.visibility=if(name in visible)android.view.View.VISIBLE else android.view.View.GONE}}
    }
    private fun formFingerprint() = listOf(endpoint.text.toString(),hostname.text.toString(),transport.selectedItemPosition,mode.selectedItemPosition,useGlobalApps.isChecked,routes.text.toString(),dns.text.toString(),ca.text.toString(),copyBudget.text.toString()).joinToString("\u0000")
    private fun dirty() = !initialising && (pendingIdentity != null || prefs.dirty || formFingerprint()!=initialForm || apps!=initialApps)
    private fun leaveDraft(next:()->Unit) {
        if(busy)return
        if(!dirty()){next();return}
        AlertDialog.Builder(this).setTitle("Сохранить изменения?").setMessage("Изменения профиля ещё не применены.")
            .setPositiveButton("Сохранить"){_,_->saveEditor(next)}
            .setNegativeButton("Не сохранять"){_,_->pendingIdentity=null;prefs=PreferenceDraft(VpnProfiles.preferences(this,profileId));draftGlobalApps=VpnProfiles.globalApps(this);buildEditor();next()}
            .setNeutralButton("Продолжить редактирование",null).show()
    }
    private fun leaveEditor() = leaveDraft { finish() }
    @Deprecated("Legacy back dispatch") override fun onBackPressed() { leaveEditor() }
    private fun saveEditor(after:()->Unit = {}) {
        if(busy)return
        if(!dirty()){Toast.makeText(this,"Нет изменений",Toast.LENGTH_SHORT).show();after();return}
        try {save()} catch(e:Exception){error(e);return}
        if(LabVpnService.active) {
            AlertDialog.Builder(this).setTitle("Применить и переподключить?").setMessage("VPN будет остановлен и запущен заново. Все текущие соединения, включая другие выходы, прервутся.")
                .setNegativeButton("Отмена",null).setPositiveButton("Применить и переподключить"){_,_->
                    busy=true
                    requestedOrientation=android.content.pm.ActivityInfo.SCREEN_ORIENTATION_LOCKED
                    startService(Intent(this,LabVpnService::class.java).setAction("stop"))
                    val until=android.os.SystemClock.elapsedRealtime()+10000
                    val wait=object:Runnable {override fun run(){
                        if(isDestroyed)return
                        if(LabVpnService.active && android.os.SystemClock.elapsedRealtime()<until){handler.postDelayed(this,100);return}
                        busy=false
                        requestedOrientation=android.content.pm.ActivityInfo.SCREEN_ORIENTATION_UNSPECIFIED
                        try {check(!LabVpnService.active){"VPN ещё останавливается. Повторите сохранение."};persistEditor();startForegroundService(Intent(this@ProfileEditorActivity,LabVpnService::class.java));after()}catch(e:Exception){error(e)}
                    }};handler.post(wait)
                }.show()
        } else try {persistEditor();after()}catch(e:Exception){error(e)}
    }
    private fun persistEditor() { synchronized(MdmConfiguration.lock){
        prefs.validateWrite()
        check(!LabVpnService.active){"Сначала остановите VPN"}
        save();pendingIdentity?.let { VpnIdentity.writeBundle(this,profileId,it) };prefs.persist();pendingIdentity=null
        if(useGlobalApps.isChecked && apps!=VpnProfiles.globalApps(this)) VpnProfiles.setGlobalApps(this,apps)
        initialForm=formFingerprint();initialApps=apps.toSet();draftGlobalApps=VpnProfiles.globalApps(this)
        Toast.makeText(this,"Изменения сохранены",Toast.LENGTH_SHORT).show()
    }
    }
    private fun refreshEditor() {prefs=PreferenceDraft(VpnProfiles.preferences(this,profileId));buildEditor()}

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
                        try {ProfileUpdate.apply(this,id,incoming);refreshEditor()}catch(e:Exception){error(e)}
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
    private fun updateTransportControls() {
        val kind=selectedTransport()
        val managed=prefs.getBoolean("managed_profile",false)
        hostname.isEnabled=!managed && kind !in setOf("awg","vless")
        endpoint.isEnabled=!managed && kind!="vless"
        ca.isEnabled=!managed && kind!="vless"
        demuxSwitch.isEnabled=kind in setOf("quic","https")
        maximumSwitch.isEnabled=demuxSwitch.isEnabled
    }
    private fun save() {
        val copies=copyBudget.text.toString().toLongOrNull()
        require(copies!=null && copies in 0..65536){"Бюджет копий: целое число от 0 до 65536 KiB/мин"}
        prefs.edit().putLong("bond_copy_kib",copies).apply()
        require(endpoint.text.contains(':')) { "Укажите домен:порт" }
        if (selectedTransport() !in setOf("awg", "vless")) require(hostname.text.isNotBlank()) { "Укажите TLS hostname" }
        if (mode.selectedItemPosition == 1)
            require(apps.isNotEmpty()) { "Выберите хотя бы одно приложение" }
        if (mode.selectedItemPosition == 3) VpnRoutes.parse(routes.text.toString())
        else VpnRoutes.parse(dns.text.toString() + "/32")
        val keys = pendingIdentity ?: VpnIdentity.load(this,profileId)
        require(when(selectedTransport()) { "awg" -> keys.optString("awg_config").isNotBlank(); "vless" -> keys.optString("vless_config").isNotBlank(); else -> keys.has("certificate") }) { "Для другого протокола импортируйте отдельный профиль" }
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

    private fun updateAppsSummary() {
        appsSummary.text =
            "${if(useGlobalApps.isChecked) "Общий список" else "Список профиля"}: ${apps.size} приложений"
    }

    private fun chooseApps() {
        startActivityForResult(
            Intent(this, AppSelectionActivity::class.java)
                .putStringArrayListExtra("apps", ArrayList(apps))
                .putExtra(
                    "title",
                    if (useGlobalApps.isChecked) "Общие приложения"
                    else "Приложения · ${VpnProfiles.list(this).first { it.id==profileId }.name}",
                ),
            13,
        )
    }

    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (resultCode != RESULT_OK) return
        if (requestCode == 13) {
            apps = (data?.getStringArrayListExtra("apps") ?: return).toMutableSet()
            if (useGlobalApps.isChecked) draftGlobalApps=apps.toSet()
            else prefs.edit().putStringSet("apps", apps).apply()
            updateAppsSummary()
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
                        pendingIdentity = VpnIdentity.parsePkcs12(bytes, password.text.toString().toCharArray())
                        identity.text = "Сертификат подготовлен · нажмите «Сохранить»"
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
