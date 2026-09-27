package ru.vpnc.quiclab

import android.graphics.BitmapFactory
import android.net.ConnectivityManager
import android.os.ParcelFileDescriptor
import androidx.test.platform.app.InstrumentationRegistry
import com.google.zxing.BinaryBitmap
import com.google.zxing.RGBLuminanceSource
import com.google.zxing.common.HybridBinarizer
import com.google.zxing.qrcode.QRCodeReader
import mobile.EventSink
import mobile.Mobile
import mobile.SocketBinder
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Test
import java.io.File
import java.net.Inet4Address
import java.util.concurrent.atomic.AtomicInteger

class CaptureSmokeTest {
    @Test fun echoSecretsAreUploadedForBothTransports() {
        val context=InstrumentationRegistry.getInstrumentation().targetContext
        val source=File(context.filesDir,"capture-smoke-qr.png")
        org.junit.Assume.assumeTrue("Provision an ephemeral capture QR first",source.exists())
        val bitmap=BitmapFactory.decodeFile(source.path)
        val pixels=IntArray(bitmap.width*bitmap.height);bitmap.getPixels(pixels,0,bitmap.width,0,0,bitmap.width,bitmap.height)
        val raw=QRCodeReader().decode(BinaryBitmap(HybridBinarizer(RGBLuminanceSource(bitmap.width,bitmap.height,pixels)))).text
        val profile=if(raw.startsWith("https://")) ProfileImport.fetch(raw) else JSONObject(raw)
        source.delete()
        assertEquals("capture",ProfileImport.save(context,profile))
        val prefs=context.getSharedPreferences("server",0)
        val host=prefs.getString("hostname","").orEmpty()
        assertEquals(profile.getString("hostname"),host)
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
            val status=Mobile.debugCaptureStatus()
            val sent=Regex("отправлено ([0-9]+)").find(status)?.groupValues?.get(1)?.toInt() ?: 0
            assertTrue("Expected secrets from both TLS connections",sent>=8)
            println("Echo replies: QUIC=${quicReplies.get()}, HTTPS=${httpsReplies.get()}; uploaded secret lines=$sent")
        } finally {quic.stop();https.stop();Mobile.stopDebugCapture()}
    }
}
