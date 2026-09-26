package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.net.VpnService
import android.os.Bundle
import android.os.Handler
import android.os.Looper
import android.widget.*

class MultipleVpnActivity : Activity() {
    private val handler = Handler(Looper.getMainLooper())

    private fun action(body: () -> Unit) {
        try {
            body()
        } catch (e: Exception) {
            AlertDialog.Builder(this).setMessage(e.message).setPositiveButton("OK", null).show()
        }
    }

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        render()
    }

    private fun render() {
        handler.removeCallbacksAndMessages(null)
        val root =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setPadding(32, 48, 32, 32)
            }
        setContentView(ScrollView(this).apply { addView(root) })
        fun text(s: String) =
            TextView(this).apply {
                text = s
                textSize = 16f
                setPadding(0, 16, 0, 16)
                root.addView(this)
            }
        fun button(s: String, body: () -> Unit) {
            root.addView(
                Button(this).apply {
                    text = s
                    isAllCaps = false
                    setOnClickListener { action(body) }
                }
            )
        }
        button("‹ Назад") { finish() }
        text("Несколько VPN одновременно").textSize = 24f
        root.addView(
            Switch(this).apply {
                text = "Режим multiple"
                isChecked = VpnProfiles.multiple(this@MultipleVpnActivity)
                isEnabled = !LabVpnService.active
                setOnCheckedChangeListener { _, v ->
                    action { VpnProfiles.setMultiple(this@MultipleVpnActivity, v) }
                }
            }
        )
        text(
            "Первый подходящий профиль определяет маршрут. Домашний профиль поставьте выше профиля приложений, если подсети должны иметь приоритет. При отказе или остановке профиля его маршруты блокируются; остальные работают."
        )
        text(
            "Экспериментальная сборка: проверка на телефоне ещё не выполнена. Неизвестный UID блокируется при достижении правила приложений. Приложения с общим UID получают общий маршрут. IPv6 заблокирован. Несовпавший IPv4-трафик идёт напрямую."
        )
        val enabled = VpnProfiles.enabled(this)
        VpnProfiles.list(this).forEachIndexed { index, p ->
            val prefs = VpnProfiles.preferences(this, p.id)
            root.addView(
                CheckBox(this).apply {
                    text =
                        "${index+1}. ${p.name} · ${prefs.getString("transport","quic")!!.uppercase()}"
                    isChecked = p.id in enabled
                    isEnabled = !LabVpnService.active
                    setOnCheckedChangeListener { _, v ->
                        action {
                            VpnProfiles.setEnabled(
                                this@MultipleVpnActivity,
                                if (v) VpnProfiles.enabled(this@MultipleVpnActivity) + p.id
                                else VpnProfiles.enabled(this@MultipleVpnActivity) - p.id,
                            )
                            render()
                        }
                    }
                }
            )
            val names =
                listOf("Все приложения", "Выбранные приложения", "Кроме выбранных", "Подсети")
            text(
                names.getOrElse(prefs.getInt("mode", 0)) { "Неизвестный режим" } +
                    if (prefs.getInt("mode", 0) == 3) ": ${prefs.getString("routes","")}" else ""
            )
            val row = LinearLayout(this)
            root.addView(row)
            fun control(label: String, body: () -> Unit) {
                row.addView(
                    Button(this).apply {
                        text = label
                        isAllCaps = false
                        isEnabled = !LabVpnService.active
                        setOnClickListener {
                            action {
                                body()
                                render()
                            }
                        }
                    }
                )
            }
            control("↑") { VpnProfiles.move(this, p.id, -1) }
            control("↓") { VpnProfiles.move(this, p.id, 1) }
            control("Настроить") {
                VpnProfiles.select(this, p.id)
                startActivity(Intent(this, VpnActivity::class.java))
            }
            if (LabVpnService.active && LabVpnService.multipleMode && p.id in enabled)
                button("Остановить / возобновить ${p.name}") {
                    startService(
                        Intent(this, LabVpnService::class.java)
                            .setAction("toggle-profile")
                            .putExtra("profile_id", p.id)
                    )
                }
        }
        button("Выбрать DNS-профиль") {
            check(!LabVpnService.active) { "Сначала остановите VPN" }
            val ps = VpnProfiles.list(this).filter { it.id in VpnProfiles.enabled(this) }
            require(ps.isNotEmpty()) { "Выберите профили" }
            AlertDialog.Builder(this)
                .setTitle("Общий DNS через профиль")
                .setItems(ps.map { it.name }.toTypedArray()) { _, i ->
                    action {
                        VpnProfiles.setDnsProfile(this, ps[i].id)
                        render()
                    }
                }
                .show()
        }
        val dnsId =
            VpnProfiles.dnsProfile(this).ifBlank {
                VpnProfiles.list(this).firstOrNull { it.id in enabled }?.id.orEmpty()
            }
        text(
            "DNS: ${VpnProfiles.list(this).firstOrNull{it.id==dnsId}?.name ?: "не выбран"}. DNS TCP/UDP на порт 53 направляется через этот профиль. Split DNS и Fake IP отложены; Private DNS/DoH не распределяются по приложениям через обычный DNS."
        )
        button(if (LabVpnService.active) "Остановить VPN" else "Запустить multiple") {
            if (LabVpnService.active) {
                startService(Intent(this, LabVpnService::class.java).setAction("stop"))
                return@button
            }
            VpnProfiles.setMultiple(this, true)
            MultipleVpnPlan.load(this)
            val consent = VpnService.prepare(this)
            if (consent != null) startActivityForResult(consent, 51)
            else startForegroundService(Intent(this, LabVpnService::class.java))
        }
        val status = text("")
        val wasActive = LabVpnService.active
        handler.post(
            object : Runnable {
                override fun run() {
                    if (wasActive != LabVpnService.active) {
                        render()
                        return
                    }
                    status.text = LabVpnService.status + "\n\n" + MultipleVpnState.summary()
                    handler.postDelayed(this, 1000)
                }
            }
        )
    }

    override fun onActivityResult(requestCode: Int, resultCode: Int, data: Intent?) {
        super.onActivityResult(requestCode, resultCode, data)
        if (requestCode == 51 && resultCode == RESULT_OK)
            startForegroundService(Intent(this, LabVpnService::class.java))
    }

    override fun onResume() {
        super.onResume()
        render()
    }

    override fun onDestroy() {
        handler.removeCallbacksAndMessages(null)
        super.onDestroy()
    }
}
