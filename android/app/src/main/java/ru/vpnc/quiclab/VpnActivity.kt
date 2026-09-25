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
                text = "Настройки VPN"
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
        var panel = section("ПРОФИЛЬ")
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
        label(panel, "AmneziaWG: импорт .conf или QR. RTT — ICMP ping endpoint через туннель раз в секунду. Доступные протоколы задаются при выдаче профиля.")
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
                    hostname.isEnabled = transportTypes[position] != "awg"
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
        label(
            panel,
            "Network routing использует DNS текущей сети. Совпадение локальных и удалённых подсетей не обрабатывается.",
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
            require(prefs.getString("transport", "quic") != "awg") { "Для сертификата создайте отдельный профиль QUIC/HTTPS" }
            startActivityForResult(
                Intent(Intent.ACTION_OPEN_DOCUMENT)
                    .setType("*/*")
                    .addCategory(Intent.CATEGORY_OPENABLE),
                11,
            )
        }
        ca = field(advanced, "CA сервера PEM (пусто для публичного сертификата)", "ca")
        panel = section("УПРАВЛЕНИЕ")
        button(panel, "Сохранить настройки") {
            save()
            finish()
        }
        button(panel, "Запустить VPN") {
            save()
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
        label(diagnostics, "IPv4: QUIC/HTTPS поддерживают TCP и DNS; AmneziaWG — TCP и UDP. IPv6 заблокирован для захваченного VPN трафика.")
        logs = label(diagnostics, "")
        logs.textSize = 12f
        handler.post(
            object : Runnable {
                override fun run() {
                    state.text =
                        if (LabVpnService.active)
                            "${LabVpnService.status} · RTT ${"%.0f".format(LabVpnService.rtt)} мс"
                        else LabVpnService.status
                    logs.text = LabVpnService.log()
                    handler.postDelayed(this, 500)
                }
            }
        )
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
