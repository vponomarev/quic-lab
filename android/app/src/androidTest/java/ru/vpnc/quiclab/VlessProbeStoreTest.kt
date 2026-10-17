package ru.vpnc.quiclab
import androidx.test.platform.app.InstrumentationRegistry
import org.junit.Test
import org.junit.Assert.*
import org.json.JSONObject
import org.json.JSONArray
class VlessProbeStoreTest{
 @Test fun recoveryPreservesCompletedRowsAndBoundsSources(){
 val real=InstrumentationRegistry.getInstrumentation().targetContext
 val dir=java.io.File(real.cacheDir,"probe-store-test-"+System.nanoTime()).apply{mkdirs()}
 val c=object:android.content.ContextWrapper(real){override fun getFilesDir()=dir}
 try{val s=VlessProbeStore(c);for(i in 0..32)s.save(JSONObject().put("sourceId","s$i").put("runId","r$i").put("startedAt","2026-10-17T00:00:%02dZ".format(i)).put("state","running").put("sequence",1).put("rows",JSONArray().put(JSONObject().put("outcome","success")).put(JSONObject().put("outcome","running"))))
 assertEquals(32,s.all().length());assertFalse(s.all().has("s0"));s.recover();val r=s.all().getJSONObject("s32");assertEquals("interrupted",r.getString("state"));assertEquals("success",r.getJSONArray("rows").getJSONObject(0).getString("outcome"));assertEquals("interrupted",r.getJSONArray("rows").getJSONObject(1).getString("outcome"))
 }finally{dir.deleteRecursively()}
 }
}
