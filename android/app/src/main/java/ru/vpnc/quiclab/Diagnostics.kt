package ru.vpnc.quiclab

import android.app.Activity
import android.app.AlertDialog
import android.content.ClipData
import android.content.Context
import android.content.Intent
import android.net.ConnectivityManager
import android.net.NetworkCapabilities
import android.os.Build
import android.os.SystemClock
import android.util.AtomicFile
import android.widget.ScrollView
import android.widget.TextView
import android.widget.Toast
import androidx.core.content.FileProvider
import java.io.File
import java.time.Instant
import java.util.concurrent.Executors
import java.util.concurrent.TimeUnit
import org.json.JSONArray
import org.json.JSONObject

/** Bounded, app-private journal. Configuration objects and credentials are never serialized. */
internal object Diagnostics {
    private val entries = ArrayDeque<String>()
    private val routineAt = mutableMapOf<String,Long>()
    private val ioLock = Any()
    private var journal: AtomicFile? = null
    private var revision = 0L
    private var savedRevision = -1L
    private const val LIMIT = 400

    @Synchronized fun init(context: Context) {
        if (journal != null) return
        journal = AtomicFile(File(context.filesDir, "diagnostics.json"))
        try {
            val stored = JSONArray(String(journal!!.readFully(), Charsets.UTF_8))
            for (i in maxOf(0, stored.length() - LIMIT) until stored.length()) entries.addLast(sanitize(stored.getString(i)))
        } catch (_: Exception) { }
        Executors.newSingleThreadScheduledExecutor().scheduleWithFixedDelay({ persist() }, 5, 5, TimeUnit.SECONDS)
        val previous = Thread.getDefaultUncaughtExceptionHandler()
        Thread.setDefaultUncaughtExceptionHandler { thread, error ->
            event("app", JSONObject().put("event", "uncaught_exception").put("error", "${error.javaClass.name}: ${error.message}\n${error.stackTrace.take(12).joinToString("\n")}"))
            persist()
            if (previous != null) previous.uncaughtException(thread, error)
            else android.os.Process.killProcess(android.os.Process.myPid())
        }
    }

    internal fun sanitize(value: String): String {
        if (value.contains("PRIVATE KEY", true) || value.contains("BEGIN CERTIFICATE", true)) return "[credential material omitted]"
        return value.replace(Regex("(?i)(password|token|authorization|certificate|private_key|key)[\\\"']?\\s*[:=]\\s*([\\\"'][^\\\"']*[\\\"']|[^\\s,}]+)"), "$1=[redacted]")
            .replace(Regex("(?i)(/enroll#)[A-Za-z0-9_-]+"), "$1[redacted]")
            .take(1000)
    }

    @Synchronized fun event(source: String, event: JSONObject) {
        val kind = event.optString("event")
        if (kind in listOf("echo", "transit_echo")) return
        if (kind in listOf("traffic", "udp_rejected", "standby_ready", "standby_unavailable", "probe_unavailable")) {
            val routineKey="$source/$kind"
            val time=SystemClock.elapsedRealtime()
            if (time-(routineAt[routineKey] ?: -30000L)<30000) return
            routineAt[routineKey]=time
        }
        val fields = listOf("detail", "error", "ip", "connection_id", "local", "remote", "transport", "key", "network", "destination", "up", "down", "tcp_flows", "udp_flows", "udp_tx", "udp_rx", "udp_rejected", "datagram_drops")
            .filter { event.has(it) && it != "key" }
            .joinToString(" ") { "$it=${sanitize(event.optString(it))}" }
        entries.addLast("${Instant.now()} +${SystemClock.elapsedRealtime()}ms ${sanitize(source)} ${sanitize(kind)} $fields".take(1400))
        while (entries.size > LIMIT) entries.removeFirst()
        revision++
    }

    private fun persist() = synchronized(ioLock) {
        val snapshot = synchronized(this) {
            val file = journal ?: return@synchronized null
            if (savedRevision == revision) null else Triple(file, revision, JSONArray(entries.toList()).toString().toByteArray(Charsets.UTF_8))
        } ?: return@synchronized
        var stream: java.io.FileOutputStream? = null
        try {
            stream = snapshot.first.startWrite()
            stream.write(snapshot.third)
            snapshot.first.finishWrite(stream)
            synchronized(this) { savedRevision = snapshot.second }
        } catch (_: Exception) { stream?.let { snapshot.first.failWrite(it) } }
    }

    fun report(context: Context, echoSummary: String): String {
        val packageInfo = context.packageManager.getPackageInfo(context.packageName, 0)
        val prefs = VpnProfiles.preferences(context)
        val now = SystemClock.elapsedRealtime()
        val cm = context.getSystemService(ConnectivityManager::class.java)
        val networks = cm.allNetworks.mapNotNull { network ->
            cm.getNetworkCapabilities(network)?.let { caps ->
                val transports = listOf(NetworkCapabilities.TRANSPORT_WIFI to "Wi-Fi", NetworkCapabilities.TRANSPORT_CELLULAR to "Mobile", NetworkCapabilities.TRANSPORT_VPN to "VPN", NetworkCapabilities.TRANSPORT_ETHERNET to "Ethernet").filter { caps.hasTransport(it.first) }.joinToString("+") { it.second }
                "$transports validated=${caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_VALIDATED)} internet=${caps.hasCapability(NetworkCapabilities.NET_CAPABILITY_INTERNET)}"
            }
        }
        val lines = synchronized(this) { entries.toList() }
        return buildString {
            appendLine("QUIC Lab diagnostics · ${Instant.now()}")
            appendLine("App ${packageInfo.versionName} (${packageInfo.longVersionCode}); Android ${Build.VERSION.RELEASE} API ${Build.VERSION.SDK_INT}")
            appendLine("Device ${Build.MANUFACTURER} ${Build.MODEL}; ABI ${Build.SUPPORTED_ABIS.joinToString()}")
            appendLine("Server ${sanitize(prefs.getString("endpoint", "").orEmpty())}; transport ${prefs.getString("transport", "quic")}; routing mode ${prefs.getInt("mode",0)}; app count ${VpnProfiles.apps(context).size}")
            appendLine("VPN active=${LabVpnService.active}; ${sanitize(LabVpnService.status)}; network=${LabVpnService.network}")
            appendLine("RTT=${LabVpnService.rtt}ms; last reply age=${if(LabVpnService.lastEcho>0) now-LabVpnService.lastEcho else -1}ms")
            if (LabVpnService.transitEnabled) appendLine("Transit RTT=${LabVpnService.transitRtt}ms; last reply age=${if(LabVpnService.lastTransitEcho>0) now-LabVpnService.lastTransitEcho else -1}ms")
            appendLine(LabVpnService.quality(now))
            appendLine(LabVpnService.flowSummary)
            appendLine("TX=${LabVpnService.txBytes}; RX=${LabVpnService.rxBytes}; TX/s=${LabVpnService.txRate}; RX/s=${LabVpnService.rxRate}")
            appendLine("Exit IP=${LabVpnService.exitIP}; ${LabVpnService.exitState}; check age=${if(LabVpnService.exitCheckedAt>0) now-LabVpnService.exitCheckedAt else -1}ms")
            appendLine("Networks: ${networks.joinToString("; ")}")
            appendLine(sanitize(echoSummary))
            appendLine("\nLast $LIMIT events (UTC and monotonic time). No packet payloads, keys, certificates, SSID, BSSID or cell IDs.\n")
            lines.forEach { appendLine(it) }
        }
    }

    fun preview(activity: Activity, echoSummary: String) {
        try {
            val report = report(activity, echoSummary)
            val content = TextView(activity).apply { text = report; textSize = 12f; setPadding(24,16,24,16); setTextIsSelectable(true) }
            AlertDialog.Builder(activity).setTitle("Диагностика")
                .setMessage("Отчёт содержит адрес сервера, exit IP и события сети. Проверьте перед отправкой.")
                .setView(android.widget.LinearLayout(activity).apply {
                    orientation = android.widget.LinearLayout.VERTICAL
                    val height = (activity.resources.displayMetrics.heightPixels * 0.5f).toInt()
                    addView(ScrollView(activity).apply { addView(content) }, android.widget.LinearLayout.LayoutParams(-1, height))
                })
                .setNegativeButton("Закрыть", null)
                .setPositiveButton("Поделиться") { _, _ ->
                    try {
                        val dir = File(activity.cacheDir, "diagnostics").apply { mkdirs() }
                        dir.listFiles()?.filter { it.lastModified() < System.currentTimeMillis() - 86400000 }?.forEach { it.delete() }
                        val file = File(dir,"quic-lab-${System.currentTimeMillis()}.txt").apply { writeText(report) }
                        val uri = FileProvider.getUriForFile(activity, activity.packageName+".diagnostics", file)
                        val send = Intent(Intent.ACTION_SEND).setType("text/plain").putExtra(Intent.EXTRA_STREAM,uri)
                            .putExtra(Intent.EXTRA_SUBJECT,"QUIC Lab diagnostics").addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
                        send.clipData = ClipData.newRawUri("Diagnostics",uri)
                        activity.startActivity(Intent.createChooser(send,"Поделиться диагностикой"))
                    } catch (e: Exception) { Toast.makeText(activity,"Не удалось экспортировать: ${e.message}",Toast.LENGTH_LONG).show() }
                }.show()
        } catch (e: Exception) { Toast.makeText(activity,"Не удалось собрать отчёт: ${e.message}",Toast.LENGTH_LONG).show() }
    }
}
