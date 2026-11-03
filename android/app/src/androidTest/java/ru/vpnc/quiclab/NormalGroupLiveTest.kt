package ru.vpnc.quiclab

import android.net.ConnectivityManager
import androidx.test.platform.app.InstrumentationRegistry
import mobile.Gateway
import org.json.JSONArray
import org.json.JSONObject
import org.junit.Assert.*
import org.junit.Assume.assumeTrue
import org.junit.Test
import java.io.File
import java.util.concurrent.CopyOnWriteArrayList
import java.util.concurrent.CountDownLatch
import java.util.concurrent.LinkedBlockingQueue
import java.util.concurrent.TimeUnit

/** Real VpnSession and native paths, without TUN, VPN consent or saved-profile writes. */
class NormalGroupLiveTest {
    @Test fun sustainedHttpsDegradationSwitchesOnlyAfterCandidateChecks() {
        val args=InstrumentationRegistry.getArguments()
        assumeTrue("opt-in Linux fixture", args.getString("normal_group_live")=="true")
        val context=InstrumentationRegistry.getInstrumentation().targetContext
        val rows=JSONArray(File(context.getExternalFilesDir(null),"normal-group-fixture.json").readText())
        val candidates=JSONArray()
        for(i in 0 until rows.length()) candidates.put(JSONObject().put("connection_id","fixture-$i").put("gateway",rows.getJSONObject(i)))
        val config=JSONObject(rows.getJSONObject(0).toString()).put(AdaptiveGroups.CANDIDATES,candidates)
        val cm=context.getSystemService(ConnectivityManager::class.java)
        val ready=CountDownLatch(1)
        val events=LinkedBlockingQueue<JSONObject>()
        val attached=CopyOnWriteArrayList<Gateway>()
        val detached=LinkedBlockingQueue<Gateway>()
        val session=VpnSession(cm,null,config,-1,
            availability={_,kind,available->if(kind==VpnSession.WIFI && available)ready.countDown()},
            attached={attached.add(it)},detached={detached.offer(it)},output={events.offer(it)})
        try {
            assertTrue("physical Wi-Fi unavailable",ready.await(10,TimeUnit.SECONDS))
            session.startOrMigrate(VpnSession.WIFI,config.getString("endpoint"),"localhost","",1000)
            val deadline=android.os.SystemClock.elapsedRealtime()+145000
            var switched:JSONObject?=null
            while(android.os.SystemClock.elapsedRealtime()<deadline) {
                val e=events.poll(1,TimeUnit.SECONDS)?:continue
                assertFalse("unexpected controller failure: $e",e.optString("event")=="controller_error")
                if(e.optString("event")=="vless_group" && e.optString("reason")=="sustained_degradation") {switched=e;break}
            }
            assertNotNull("no sustained-degradation switch; attached=${attached.size}",switched)
            assertEquals(2,switched!!.getInt("index"))
            assertEquals("https",switched.getString("transport"))
            assertEquals("background candidate was attached to the working route",2,attached.size)
            val check=attached.last().newPathProbe()
            try { assertTrue("new route cannot complete verified HTTPS",check.measure()>0) } finally {check.stop()}
        } finally {
            session.close()
            repeat(attached.size) { assertNotNull("gateway not disposed",detached.poll(10,TimeUnit.SECONDS)) }
        }
    }
}
