package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.content.pm.PackageManager
import android.graphics.Color
import android.net.VpnService
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.text.InputType
import android.widget.*

class VpnActivity : Activity() {
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
    private var apps = mutableSetOf<String>()
    private val prefs by lazy { getSharedPreferences("vpn", MODE_PRIVATE) }

    private fun label(panel: LinearLayout, value: String): TextView =
        TextView(this).also {
            it.text = value
            it.textSize = 16f
            it.setPadding(0, 16, 0, 8)
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
            it.textSize = 15f
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
            panel.addView(it)
        }
    }

    private fun button(panel: LinearLayout, title: String, action: () -> Unit) {
        panel.addView(
            Button(this).apply {
                text = title
                isAllCaps = false
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
        val panel =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(24, 36, 24, 32)
                setBackgroundColor(Color.rgb(240, 245, 248))
            }
        val scroll = ScrollView(this).apply { addView(panel) }
        setContentView(scroll)
        scroll.setOnApplyWindowInsetsListener { _, ins ->
            val bars = ins.getInsets(android.view.WindowInsets.Type.systemBars())
            panel.setPadding(24 + bars.left, 24 + bars.top, 24 + bars.right, 24 + bars.bottom)
            ins
        }
        label(panel, "VPN · QUIC Lab").textSize = 28f
        button(panel,"Сканировать QR") {check(!LabVpnService.active){"Сначала остановите VPN"};startActivityForResult(Intent(this,ProfileScanActivity::class.java),ProfileImport.REQUEST)}
        label(
            panel,
            "Дополнение к echo-стенду. IPv4, TCP и DNS. Остальной UDP и IPv6 в туннеле пока недоступны.",
        )
        state = label(panel, LabVpnService.status)
        transport =
            choice(
                panel,
                "Транспорт",
                arrayOf("QUIC", "HTTPS / WebSocket"),
                if (prefs.getString("transport", "quic") == "quic") 0 else 1,
            )
        endpoint = field(panel, "Адрес туннеля: домен:порт (QUIC 4434 / HTTPS 8443)", "endpoint")
        hostname = field(panel, "TLS hostname", "hostname")
        transport.onItemSelectedListener=object:android.widget.AdapterView.OnItemSelectedListener {
            private var initial=true
            override fun onNothingSelected(parent:android.widget.AdapterView<*>?) {}
            override fun onItemSelected(parent:android.widget.AdapterView<*>?,view:android.view.View?,position:Int,id:Long){if(initial){initial=false;return};prefs.getString(if(position==0)"quic_endpoint" else "https_endpoint",null)?.let{endpoint.setText(it)}}
        }

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
        apps = prefs.getStringSet("apps", emptySet())!!.toMutableSet()
        button(panel, "Выбрать приложения") { chooseApps() }
        routes = field(panel, "Подсети для Network routing, по одной IPv4 CIDR на строку", "routes")
        dns = field(panel, "DNS для режимов приложений (IPv4)", "dns", "1.1.1.1")
        label(
            panel,
            "Network routing использует DNS текущей сети. Совпадение локальных и удалённых подсетей не обрабатывается.",
        )
        identity =
            label(
                panel,
                try {
                    VpnIdentity.load(this).optString("subject")
                } catch (_: Exception) {
                    "Клиентский сертификат не импортирован"
                },
            )
        button(panel, "Импортировать сертификат .p12") {
            startActivityForResult(
                Intent(Intent.ACTION_OPEN_DOCUMENT)
                    .setType("*/*")
                    .addCategory(Intent.CATEGORY_OPENABLE),
                11,
            )
        }
        ca = field(panel, "CA сервера PEM (пусто для публичного сертификата)", "ca")
        button(panel, "Запустить VPN") {
            save()
            val request = VpnService.prepare(this)
            if (request != null) startActivityForResult(request, 12) else startVPN()
        }
        button(panel, "Остановить VPN") { stopService(Intent(this, LabVpnService::class.java)) }
        label(
            panel,
            "Смена транспорта или маршрутов: остановите VPN, измените настройки и запустите снова. Текущие соединения завершатся.",
        )
        logs = label(panel, "")
        logs.textSize = 12f
        handler.post(
            object : Runnable {
                override fun run() {
                    state.text =
                        "${LabVpnService.status}\nRTT ${"%.0f".format(LabVpnService.rtt)} мс\nСеанс ${LabVpnService.connection}"
                    logs.text = LabVpnService.log()
                    handler.postDelayed(this, 500)
                }
            }
        )
    }

    private fun save() {
        check(!LabVpnService.active) { "Сначала остановите VPN" }
        require(endpoint.text.contains(':')) { "Укажите домен:порт" }
        require(hostname.text.isNotBlank()) { "Укажите TLS hostname" }
        if (mode.selectedItemPosition == 1)
            require(apps.isNotEmpty()) { "Выберите хотя бы одно приложение" }
        if (mode.selectedItemPosition == 3) VpnRoutes.parse(routes.text.toString())
        else VpnRoutes.parse(dns.text.toString() + "/32")
        VpnIdentity.load(this)
        prefs
            .edit()
            .putString("transport", if (transport.selectedItemPosition == 0) "quic" else "https")
            .putString("endpoint", endpoint.text.toString().trim())
            .putString("hostname", hostname.text.toString().trim())
            .putInt("mode", mode.selectedItemPosition)
            .putStringSet("apps", apps)
            .putString("routes", routes.text.toString())
            .putString("dns", dns.text.toString().trim())
            .putString("ca", ca.text.toString())
            .apply()
    }

    private fun startVPN() {
        startForegroundService(Intent(this, LabVpnService::class.java))
    }

    private fun chooseApps() {
        val choices =
            packageManager
                .getInstalledApplications(PackageManager.GET_META_DATA)
                .filter { it.packageName != packageName }
                .sortedBy { packageManager.getApplicationLabel(it).toString().lowercase() }
        val selected = apps.toMutableSet()
        AlertDialog.Builder(this)
            .setTitle("Приложения (${choices.size})")
            .setMultiChoiceItems(
                choices
                    .map { "${packageManager.getApplicationLabel(it)}\n${it.packageName}" }
                    .toTypedArray(),
                choices.map { it.packageName in apps }.toBooleanArray(),
            ) { _, i, on ->
                if (on) selected.add(choices[i].packageName)
                else selected.remove(choices[i].packageName)
            }
            .setPositiveButton("Сохранить") { _, _ -> apps = selected }
            .setNegativeButton("Отмена", null)
            .show()
    }

    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (resultCode != RESULT_OK) return
        if(requestCode==ProfileImport.REQUEST){recreate();return}
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
