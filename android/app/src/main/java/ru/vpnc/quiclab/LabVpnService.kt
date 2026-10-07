package ru.vpnc.quiclab

import android.app.*
import android.content.Intent
import android.net.ConnectivityManager
import android.net.VpnService
import android.os.Handler
import android.os.Looper
import android.os.ParcelFileDescriptor
import org.json.JSONObject

class LabVpnService : VpnService() {
    internal class UpdateConsentRequired: IllegalStateException("Нужно разрешение на служебную загрузку сверх LTE-бюджета")
    internal val budgetRun by lazy { VpnBudgetRun.forContext(this) }
    private var session: VpnSession? = null
    private var exits: VpnExitController<VpnSession>? = null
    private var multiple: MultipleVpnController? = null
    private var singleRouter: mobile.MultiRouter? = null
    private var singleRouterToken: Any? = null
    private var dnsPolicy: VpnDnsPolicy? = null
    private var tun: ParcelFileDescriptor? = null
    private val handler = Handler(Looper.getMainLooper())
    private var starting = false

    override fun onStartCommand(intent: Intent?, flags: Int, startId: Int): Int {
        liveService=this
        Diagnostics.init(applicationContext)
        if (intent?.action == "toggle-profile") {
            multiple?.toggle(intent.getStringExtra("profile_id").orEmpty());return restartPolicy()
        }
        if (intent?.action == "exit-ip") {
            if (active && exitEnabled) session?.checkExitIP()
            return restartPolicy()
        }
        if (intent?.action == "stop") {
            Diagnostics.event("vpn", JSONObject().put("event", "stop_requested"))
            shutdown(clearBudget=true)
            val local = getSharedPreferences("vpn_lifecycle", MODE_PRIVATE)
            if (!local.getBoolean("stop_notice_shown", false)) {
                android.widget.Toast.makeText(this, "VPN остановлен. Блокировка снята: приложения могут подключаться напрямую.", android.widget.Toast.LENGTH_LONG).show()
                local.edit().putBoolean("stop_notice_shown", true).apply()
            }
            stopSelf()
            return START_NOT_STICKY
        }
        if (intent?.action == "move") {
            if (active) {
                multiple?.let { it.move(intent.getIntExtra("network",VpnSession.WIFI));return restartPolicy() }
                val p=VpnProfiles.preferences(this)
                session?.startOrMigrate(intent.getIntExtra("network",VpnSession.WIFI),p.getString("endpoint","")!!,p.getString("hostname","")!!,"",100)
            }
            return restartPolicy()
        }
        if (session != null || multiple != null) return restartPolicy()
        if(intent==null && !budgetRun.shouldResume())return restartPolicy()
        Diagnostics.event("vpn", JSONObject().put("event", if (intent == null) "system_restart" else "start_requested"))
        resetMetrics(VpnProfiles.preferences(this).getString("transport","quic")!!)
        transitEnabled = !VpnProfiles.preferences(this).getString("transit_endpoint", "").isNullOrBlank()
        connection = "—"
        rtt = 0.0
        val manager = getSystemService(NotificationManager::class.java)
        manager.createNotificationChannel(
            NotificationChannel("vpn", "QUIC Lab VPN", NotificationManager.IMPORTANCE_LOW)
        )
        val open =
            PendingIntent.getActivity(
                this,
                0,
                Intent(this, VpnActivity::class.java),
                PendingIntent.FLAG_IMMUTABLE,
            )
        multipleMode = false
        status = "Подключаем…"
        startForeground(42, vpnNotification())
        handler.postDelayed(notificationTick, 1000)
        try {
            // One shared meter survives every exit reconnect in this VPN run.
            val runBudget = budgetRun.start(VpnBudgetSettings.limitBytes(this))
            if(budgetRun.restored)Diagnostics.event("vpn",JSONObject().put("event","budget_restored").put("snapshot",JSONObject(runBudget.snapshot())))
            val dashboardRunId=JSONObject(runBudget.snapshot()).getString("epoch")
            if (VpnProfiles.multiple(this)) {
                val plan=MultipleVpnPlan.load(this)
                VpnDashboardEvents.beginRun(dashboardRunId,plan.profiles.associate {it.id to it.name}) {runBudget.snapshot()}
                tun=Builder().setSession("QUIC Lab · multiple").setMtu(1280).addAddress("10.254.254.1",32)
                    .addRoute("0.0.0.0",0).addDnsServer(plan.dns).addDisallowedApplication(packageName)
                    .setBlocking(true).setConfigureIntent(open).establish() ?: error("VPN не разрешён")
                budgetRun.setResumeEligible(true)
                active=true; multipleMode=true;transport="multiple";exitEnabled=false;status="Несколько VPN · ${plan.profiles.size} профилей"
                multiple=MultipleVpnController(this,plan,tun!!.fd,runBudget)
                updateRuntime=VpnProfileUpdateRuntime(plan.dnsProfile,runBudget,requireNotNull(tun)) {id-> handler.post {runCatching {multiple?.restart(id)}.onFailure {MultipleVpnState.record(id,JSONObject().put("event","operation_failed"))}}}
                return restartPolicy()
            }
            multipleMode=false
            val prefs = VpnProfiles.preferences(this)
            val mode = prefs.getInt("mode", 0)
            exitEnabled = mode != 3
            exitState = if (exitEnabled) "Ожидаем туннель" else "Не проверяется в режиме подсетей"
            val apps = VpnProfiles.apps(this).filter { it != packageName }
            val builder =
                Builder()
                    .setSession("QUIC Lab VPN")
                    .setMtu(1280)
                    .addAddress("10.254.254.1", 32)
                    .setBlocking(true)
                    .setConfigureIntent(open)
            if (mode == 3)
                VpnRoutes.parse(prefs.getString("routes", "")!!).forEach {
                    builder.addRoute(it.first, it.second)
                }
            else {
                builder.addRoute("0.0.0.0", 0)
            }
            when (mode) {
                1 -> {
                    require(apps.isNotEmpty()) { "Выберите приложения" }
                    apps.forEach { builder.addAllowedApplication(it) }
                }
                2 -> {
                    (apps + packageName).forEach { builder.addDisallowedApplication(it) }
                }
                else -> builder.addDisallowedApplication(packageName)
            }
            // Do not allowFamily(AF_INET6) or add IPv6 addresses: VpnService blocks the
            // unconfigured family for captured apps; the IPv4 router also rejects IPv6.
            builder.addDnsServer(prefs.getString("dns", "1.1.1.1")!!)
            builder.addRoute(prefs.getString("dns", "1.1.1.1")!!,32)
            tun = builder.establish() ?: error("VPN не разрешён")
            val cfg =
                VpnIdentity.load(this)
                    .put("transport", prefs.getString("transport", "quic"))
                    .put("max_availability", prefs.getBoolean("max_availability", false))
                    .put("bond_copy_budget", prefs.getLong("bond_copy_kib",256).coerceIn(0,65536)*1024/2)
                    .put("bond_cell_budget", 0) // Shared physical socket budget replaces direction split.
                    .put("transit_endpoint", prefs.getString("transit_endpoint", ""))
                    .put("server_name", prefs.getString("server_name", ""))
                    .put("verify_name", prefs.getString("verify_name", ""))
                    .put("control_url", prefs.getString("control_url", ""))
                    .put("android_version_code", BuildConfig.VERSION_CODE)
                    .put("data_version", prefs.getInt("data_version", 0))
                    .put("probe_exit_ip", true)
                    .put("ca", prefs.getString("ca", ""))
                    .put("dns", prefs.getString("dns", "1.1.1.1"))
            val endpoint = prefs.getString("endpoint", "")!!
            val hostname = prefs.getString("hostname", "")!!
            val cm = getSystemService(ConnectivityManager::class.java)
            val exitId = VpnProfiles.current(this).id
            VpnDashboardEvents.beginRun(dashboardRunId,mapOf(exitId to VpnProfiles.current(this).name)) {runBudget.snapshot()}
            VpnProfiles.configuration(this)
            val dnsOwner=VpnDnsPolicy(this,VpnProfiles.dnsMode(this),exitId)
            dnsPolicy=dnsOwner
            val routerToken=Any()
            singleRouterToken=routerToken
            val router=mobile.Mobile.createMultiRouterWithDNS(
                org.json.JSONArray().put(JSONObject().put("id",exitId).put("mode","all")).toString(),
                dnsOwner.snapshot(null).toString(), null,
                object:mobile.SocketBinder {override fun bind(fd:Long) {check(protect(fd.toInt())) {"Cannot protect direct socket"}}},
                object:mobile.EventSink {
                    override fun onEvent(eventJSON:String) {
                        val event=JSONObject(eventJSON)
                        if(event.optString("event")=="profile_traffic" && event.optString("profile_id")!=exitId) return
                        handler.post {if(singleRouterToken===routerToken) {
                            VpnDashboardEvents.record(dashboardRunId,exitId,exits?.token(exitId) ?: 0L,event)
                            record(event)
                        }}
                    }
                },
            )
            singleRouter=router
            router.setTrafficBudget(runBudget)
            dnsOwner.attach(router)
            router.attach(tun!!.fd.toLong())
            lateinit var controller: VpnExitController<VpnSession>
            controller = VpnExitController(setOf(exitId)) { id, token ->
            val prefs = VpnProfiles.preferences(this,id)
            val cfg =
                VpnIdentity.load(this,id)
                    .put("transport", prefs.getString("transport", "quic"))
                    .put("max_availability", prefs.getBoolean("max_availability", false))
                    .put("bond_copy_budget", prefs.getLong("bond_copy_kib",256).coerceIn(0,65536)*1024/2)
                    .put("bond_cell_budget", 0) // Shared physical socket budget replaces direction split.
                    .put("transit_endpoint", prefs.getString("transit_endpoint", ""))
                    .put("server_name", prefs.getString("server_name", ""))
                    .put("verify_name", prefs.getString("verify_name", ""))
                    .put("control_url", prefs.getString("control_url", ""))
                    .put("android_version_code", BuildConfig.VERSION_CODE)
                    .put("data_version", prefs.getInt("data_version", 0))
                    .put("probe_exit_ip", true)
                    .put("ca", prefs.getString("ca", ""))
                    .put("dns", prefs.getString("dns", "1.1.1.1"))
            VpnConfiguration.attachRuntime(this,id,cfg)
            val endpoint = prefs.getString("endpoint", "")!!
            val hostname = prefs.getString("hostname", "")!!

                VpnSession(
                    cm,
                    this,
                    cfg,
                    -1,
                    budget = runBudget,
                    selected = { n, kind -> handler.post { controller.withCurrent(id, token) { if(!cfg.optBoolean("max_availability")) setUnderlyingNetworks(arrayOf(n)); dnsOwner.selected(n,kind) } } },
                    availability = { _, kind, up ->
                        if (up)
                            handler.post {
                                if (active && !starting && controller.current(id, token)) {
                                    starting = true
                                    session?.startOrMigrate(kind, endpoint, hostname, "", 100)
                                }
                            }
                    },
                    attached = { g -> check(controller.withCurrent(id,token) {router.setGateway(id,g)}) {"Exit stopped"} },
                    detached = { g -> router.detachGateway(id,g) },
                    output = { event ->
                        val type = event.optString("event")
                        handler.post {
                            controller.withCurrent(id, token) {
                                if (type == "operation_failed") starting = false
                                val scoped=JSONObject(event.toString()).put("exit_id",id)
                                    .put("runtime_generation",event.optLong("generation")).put("generation",token)
                                VpnDashboardEvents.record(dashboardRunId,id,token,scoped)
                                controller.event(id, token, type)
                                record(event)
                            }
                        }
                    },
                )
            }
            exits = controller
            controller.start(exitId)
            session = controller.session(exitId)
            budgetRun.setResumeEligible(true)
            active = true
            updateRuntime=VpnProfileUpdateRuntime(exitId,runBudget,requireNotNull(tun)) {id->handler.post {
                if(id==exitId && active) try {singleRouter?.blockExit(id);starting=false;controller.start(id);session=controller.session(id);status="Переподключаем обновлённый выход…"}
                catch(_:Exception) {status="Обновлённый выход заблокирован: переподключение не удалось"}
            }}
            status = "Подключаем ${cfg.optString("transport").uppercase()}…"
        } catch (e: Exception) {
            status = "Ошибка: ${e.message}"
            Diagnostics.event("vpn", JSONObject().put("event","start_failed").put("error",e.toString()))
            runCatching{budgetRun.setResumeEligible(false)}
            shutdown()
            stopSelf()
        }
        return restartPolicy()
    }

    // Every start command updates ActivityManager policy, including notification actions.
    // Explicit stop/revoke and failed starts must remain stopped.
    private fun restartPolicy(): Int {
        if (active) return START_STICKY
        stopSelf()
        return START_NOT_STICKY
    }

    private val notificationTick = object : Runnable {
        override fun run() {
            if (!active) return
            runCatching{budgetRun.checkpoint()}.onFailure{Diagnostics.event("vpn",JSONObject().put("event","budget_checkpoint_failed"))}
            getSystemService(NotificationManager::class.java).notify(42, vpnNotification())
            handler.postDelayed(this, 2000)
        }
    }

    private fun vpnNotification(): Notification {
        val now = android.os.SystemClock.elapsedRealtime()
        val content = if (multipleMode) MultipleVpnState.notification(now) else notificationSnapshot(now)
        val open = PendingIntent.getActivity(this, 2, Intent(this, VpnActivity::class.java), PendingIntent.FLAG_IMMUTABLE)
        val stop = PendingIntent.getService(this, 1, Intent(this, LabVpnService::class.java).setAction("stop"), PendingIntent.FLAG_IMMUTABLE)
        return Notification.Builder(this, "vpn")
            .setSmallIcon(android.R.drawable.ic_lock_lock)
            .setContentTitle(content.title)
            .setContentText(content.text)
            .setStyle(Notification.BigTextStyle().bigText(content.details))
            .setContentIntent(open)
            .addAction(Notification.Action.Builder(null, "Остановить", stop).build())
            .setOnlyAlertOnce(true)
            .setShowWhen(false)
            .setOngoing(true)
            .build()
    }

    override fun onRevoke() {
        Diagnostics.event("vpn", JSONObject().put("event","permission_revoked"))
        shutdown(clearBudget=true)
        stopSelf()
    }

    // Android may keep VpnService bound after stopSelf; release the VPN explicitly.
    private fun shutdown(clearBudget:Boolean=false) {
        if (active) Diagnostics.event("vpn", JSONObject().put("event","stopped"))
        updateRuntime=null
        active = false
        exitState = "VPN остановлен"
        handler.removeCallbacksAndMessages(null)
        runCatching { multiple?.close() }.onFailure { Diagnostics.event("vpn", JSONObject().put("event", "close_failed").put("error", it.toString())) }
        multiple = null
        runCatching { dnsPolicy?.close() }
        dnsPolicy = null
        singleRouterToken=null
        val closingRouter=singleRouter
        singleRouter=null
        if(closingRouter!=null) Thread({closingRouter.close()},"single-router-close").start()
        runCatching { exits?.stopAll() }.onFailure { Diagnostics.event("vpn", JSONObject().put("event", "close_failed").put("error", it.toString())) }
        exits = null
        session = null
        VpnDashboardEvents.clearRun()
        if(clearBudget)budgetRun.stop() else runCatching{budgetRun.release()}.onFailure{Diagnostics.event("vpn",JSONObject().put("event","budget_checkpoint_failed"))}
        runCatching { tun?.close() }
        tun = null
        starting = false
        txRate=0.0; rxRate=0.0
        connection = "—"
        rtt = 0.0
        stopForeground(STOP_FOREGROUND_REMOVE)
        if (!status.startsWith("Ошибка")) status = "VPN остановлен"
    }

    override fun onDestroy() {
        shutdown()
        if(liveService===this) liveService=null
        super.onDestroy()
    }

    companion object {
        @Volatile private var liveService: LabVpnService? = null
        /** Diagnostics never create a service, a VPN run, or a replacement exit. */
        internal fun echoExits(): Map<String,String> {
            val service = liveService?.takeIf { active } ?: return emptyMap()
            return VpnDashboardEvents.names().filterKeys { id ->
                (service.multiple?.echoSession(id) ?: service.exits?.session(id)) != null
            }
        }
        internal fun startExitEcho(exitId: String, target: String, intervalMs: Long,
                                   output: (JSONObject) -> Unit): AutoCloseable {
            val service = liveService?.takeIf { active } ?: error("VPN выключен")
            val session = service.multiple?.echoSession(exitId) ?: service.exits?.session(exitId)
                ?: error("Выбранный VPN-выход недоступен")
            val generation = service.multiple?.echoGeneration(exitId) ?: service.exits?.token(exitId)
                ?: error("Выход остановлен")
            val runId = VpnDashboardEvents.runId ?: error("VPN выключен")
            return session.startExitEcho(target,intervalMs) { event ->
                val current = service.multiple?.echoSession(exitId) ?: service.exits?.session(exitId)
                if (event.optBoolean("terminal") ||
                    liveService === service && active && current === session && VpnDashboardEvents.runId == runId) {
                    output(JSONObject(event.toString()).put("exit_id",exitId).put("generation",generation).put("run_id",runId))
                }
            }
        }
        private val offlineBudget = VpnBudgetRun()

        @Volatile internal var updateRuntime: VpnProfileUpdateRuntime? = null
        internal fun isActiveDnsExit(context:android.content.Context,id:String):Boolean = updateRuntime?.dnsExitId==id
        internal fun restartUpdatedExit(context:android.content.Context,id:String) {updateRuntime?.restartExit(id)}
        internal fun fetchUpdate(context:android.content.Context,id:String,kind:String,output:java.io.File,
            allowOverBudget:Boolean=false,authenticatedAPKURL:String?=null) {
            val service=liveService?.takeIf {active}
            val budget=service?.budgetRun?.current ?: synchronized(offlineBudget) {
                offlineBudget.start(VpnBudgetSettings.limitBytes(context))
            }
            val cm=context.getSystemService(ConnectivityManager::class.java)
            val networks=cm.allNetworks.mapNotNull {n->cm.getNetworkCapabilities(n)?.let {n to it}}
                .filter { !it.second.hasTransport(android.net.NetworkCapabilities.TRANSPORT_VPN) }
            val selected=networks.firstOrNull {it.second.hasTransport(android.net.NetworkCapabilities.TRANSPORT_WIFI)}
                ?: networks.firstOrNull {it.second.hasTransport(android.net.NetworkCapabilities.TRANSPORT_CELLULAR)}
                ?: error("Нет физической сети Wi-Fi/LTE")
            val snapshot=JSONObject(budget.snapshot())
            val cell=selected.second.hasTransport(android.net.NetworkCapabilities.TRANSPORT_CELLULAR)
            if(cell && !allowOverBudget && snapshot.optBoolean("blocked")) throw UpdateConsentRequired()
            try {
                ServiceTransfer(context,budget,service).fetch(java.util.UUID.randomUUID().toString(),id,kind,output,
                    selected.first,allowOverBudget,authenticatedAPKURL)
            } catch(e:Exception) {
                if(cell && !allowOverBudget && e.message?.contains("budget",true)==true) throw UpdateConsentRequired()
                throw e
            }
        }
        @Volatile var multipleMode = false
        @Volatile var exitEnabled = true
        @Volatile var exitIP = ""
        @Volatile var exitState = "Не проверен"
        @Volatile var exitCheckedAt = 0L
        @Volatile var active = false
        @Volatile var status = "VPN выключен"
        private var vlessStatus: VlessConnectionStatus? = null
        @Volatile var transitEnabled = false
        @Volatile var transitRtt = 0.0
        @Volatile var lastTransitEcho = 0L
        @Volatile var rtt = 0.0
        @Volatile var connection = "—"
        @Volatile var transport = "quic"
        @Volatile var startedAt = 0L
        @Volatile var lastEcho = 0L
        @Volatile var network = "—"
        @Volatile var txBytes = 0L
        @Volatile var rxBytes = 0L
        @Volatile var txRate = 0.0
        @Volatile var rxRate = 0.0
        @Volatile var lastTransition = 0L
        @Volatile var flowSummary = ""
        @Volatile var rttEnabled = true
        @Volatile var rttInterval = 1000L
        @Volatile var lastHealth = 0L
        @Volatile var bondSnapshot: JSONObject? = null
        fun rttFresh(now: Long) = rttEnabled && lastEcho > 0 && now-lastEcho < maxOf(3500L,rttInterval*3)
        fun healthFresh(now: Long) = lastHealth > 0 && now-lastHealth < maxOf(15000L,rttInterval*4)
        fun rttLabel(now: Long) = if(!rttEnabled) "выключен" else if(rttFresh(now)) "%.0f мс".format(rtt) else "—"

        private var trafficAt = 0L
        private var stats = TransportStats()
        private val events = ArrayDeque<String>()
        @Synchronized private fun resetMetrics(value:String) {
            flowSummary=""
            bondSnapshot=null
            lastHealth=0; rttEnabled=true; rttInterval=1000
            transitEnabled=false; transitRtt=0.0; lastTransitEcho=0
            exitIP=""; exitCheckedAt=0; exitState="Не проверен"
            vlessStatus = if(value == "vless") VlessConnectionStatus() else null
            transport=value; startedAt=android.os.SystemClock.elapsedRealtime()
            lastEcho=0; network="—"; txBytes=0; rxBytes=0; txRate=0.0; rxRate=0.0
            lastTransition=0; trafficAt=startedAt; stats=TransportStats(); events.clear()
        }
        @Synchronized internal fun notificationSnapshot(now: Long): VpnNotificationContent {
            val fresh = healthFresh(now)
            val rateFresh = trafficAt > 0 && now - trafficAt < 3500
            val rate = vpnTrafficRate(if(rateFresh) txRate else 0.0, if(rateFresh) rxRate else 0.0)
            val latency = "RTT ${rttLabel(now)}"
            val state = if(lastEcho > 0 && !fresh) "Нет свежих ответов · $status" else status
            return VpnNotificationContent(
                "${vpnNetworkLabel(network)} · ${transport.uppercase()}",
                "$rate · $latency",
                "$state\n$rate · $latency\nВсего ↑ ${vpnBytes(txBytes.toDouble())} · ↓ ${vpnBytes(rxBytes.toDouble())}",
            )
        }

        @Synchronized fun quality(now:Long):String {
            if(!rttEnabled) return "RTT и jitter выключены · служебная проверка связи: 5 с"
            val jitter=stats.maxJitter(now)?.let{"%.1f мс".format(it)} ?: "—"
            val gap=if(lastEcho==0L) "—" else "%.0f мс".format(maxOf(stats.maxGap,if(active)(now-lastEcho).toDouble() else 0.0))
            return "Jitter max · 15 с: $jitter   ·   Пауза макс.: $gap\nСеансов: ${stats.sessions.size}"
        }

        @Synchronized
        fun record(e: JSONObject) {
            Diagnostics.event("vpn", e)
            val kind = e.optString("event")
            if (kind == "connected" && e.optString("transport") == "vless") vlessStatus = VlessConnectionStatus()
            vlessStatus?.let { it.accept(e, android.os.SystemClock.elapsedRealtime()); status = it.label(android.os.SystemClock.elapsedRealtime()) }
            if (kind == "vless_tcp") return
            when(kind) {
                "bond_stats" -> { bondSnapshot=JSONObject(e.toString()); network="Wi-Fi + LTE"; return }
                "rtt_policy" -> { rttEnabled=e.optBoolean("enabled"); rttInterval=e.optLong("interval_ms",1000); lastEcho=0; lastTransitEcho=0; stats=TransportStats(); return }
                "health" -> { lastHealth=android.os.SystemClock.elapsedRealtime(); connection=e.optString("connection_id",connection); if (vlessStatus == null) status="Туннель работает"; return }
                "transit_echo" -> { transitEnabled=true; transitRtt=e.optDouble("rtt_ms"); lastTransitEcho=android.os.SystemClock.elapsedRealtime(); return }
                "transit_probe_failed" -> { return }
                "exit_ip_checking" -> { exitState="Проверяем…"; return }
                "exit_ip" -> { exitIP=e.optString("ip"); exitCheckedAt=android.os.SystemClock.elapsedRealtime(); exitState="Проверен через туннель"; return }
                "exit_ip_failed" -> { exitState="Проверка недоступна"; return }
            }
            val now=android.os.SystemClock.elapsedRealtime()
            if(kind=="traffic" || kind=="profile_traffic") {
                flowSummary="Потоки TCP/UDP: ${e.optLong("tcp_flows")}/${e.optLong("udp_flows")} · UDP пакеты ↑${e.optLong("udp_tx")} ↓${e.optLong("udp_rx")}\nUDP отклонено потоков: ${e.optLong("udp_rejected")} · DATAGRAM локальные сбросы: ${e.optLong("datagram_drops")}"
                val tx=e.optLong("tx_bytes"); val rx=e.optLong("rx_bytes")
                val seconds=(now-trafficAt).coerceAtLeast(1)/1000.0
                txRate=(tx-txBytes).coerceAtLeast(0)/seconds
                rxRate=(rx-rxBytes).coerceAtLeast(0)/seconds
                txBytes=tx; rxBytes=rx; trafficAt=now
                return
            }
            if (kind=="standby_ready") return
            if (kind=="probe_unavailable") { if (transport == "awg") status="AmneziaWG · endpoint не отвечает на ICMP"; return }
            stats.accept(e,now)
            if(kind=="active_network") network=e.optString("detail")
            if(kind in listOf("active_network","path_switched","network_lost")) lastTransition=now
            if (kind == "echo") {
                if (transport == "awg") status = "AmneziaWG · ICMP endpoint отвечает"
                lastEcho=now
                lastHealth=now
                rtt = e.optDouble("rtt_ms")
                connection = e.optString("connection_id", connection)
                return
            }
            val detail = e.optString("detail", e.optString("error", e.optString("destination", "")))
            events.addLast("${java.time.Instant.now()}  $kind  $detail")
            while (events.size > 30) events.removeFirst()
            if (vlessStatus != null) return
            status =
                when (kind) {
                    "connected" -> "Туннель подключён"
                    "disconnected" -> "Туннель разорван"
                    "reconnecting" -> "Восстанавливаем VPN…"
                    "active_network" -> "Работает через $detail"
                    "operation_failed" -> "Ошибка: $detail"
                    else -> status
                }
        }

        @Synchronized fun log() = events.joinToString("\n")
    }
}
