package ru.vpnc.quiclab

import android.net.ConnectivityManager
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import mobile.EventSink
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.net.Inet4Address
import java.util.concurrent.atomic.AtomicInteger

class ServerCaptureSmokeTest {
    @Test fun echoWorksWithoutPhoneSecrets() {
        val context=InstrumentationRegistry.getInstrumentation().targetContext
        Mobile.stopDebugCapture()
        val prefs=context.getSharedPreferences("server",0)
        val host=prefs.getString("hostname","").orEmpty()
        val network=context.getSystemService(ConnectivityManager::class.java).activeNetwork ?: error("No network")
        val address=network.getAllByName(host).filterIsInstance<Inet4Address>().first().hostAddress
        val port=prefs.getString("endpoint","").orEmpty().substringAfterLast(':')
        val binder=object:SocketBinder { override fun bind(fd:Long) { ParcelFileDescriptor.fromFd(fd.toInt()).use{network.bindSocket(it.fileDescriptor)} } }
        val quicReplies=AtomicInteger();val httpsReplies=AtomicInteger()
        val quic=Mobile.newClient(object:EventSink {override fun onEvent(raw:String){if(JSONObject(raw).optString("event")=="echo")quicReplies.incrementAndGet()}})
        val https=Mobile.newWebSocketClient(object:EventSink {override fun onEvent(raw:String){if(JSONObject(raw).optString("event")=="echo")httpsReplies.incrementAndGet()}})
        try {
            quic.start("$address:$port",host,prefs.getString("pin","").orEmpty(),50,binder)
            https.start("$address:443",host,50,binder)
            Thread.sleep(8000)
            assertTrue("QUIC echo replies",quicReplies.get()>10)
            assertTrue("HTTPS echo replies",httpsReplies.get()>10)
            assertEquals("Phone must not export secrets", "", Mobile.debugCaptureStatus())
            println("Server-only capture: QUIC=${quicReplies.get()}, HTTPS=${httpsReplies.get()}")
        } finally {quic.stop();https.stop();Mobile.stopDebugCapture()}
    }
}
