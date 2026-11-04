package ru.vpnc.quiclab

import mobile.Mobile
import org.json.JSONObject

/** Portable draft transformation only: no Context, preferences, runtime or network. */
internal object MdmUserImport {
 const val MAX_FILE=2*1024*1024
 fun decode(bytes:ByteArray,password:String)=ConfigurationTransfer.decode(bytes,password)
 fun read(stream:java.io.InputStream):ByteArray {
  val out=java.io.ByteArrayOutputStream();val chunk=ByteArray(8192)
  try{while(true){val n=stream.read(chunk);if(n<0)break;require(out.size()+n<=MAX_FILE);out.write(chunk,0,n)};return out.toByteArray()}
  finally{chunk.fill(0)}
 }
 fun prepare(draft:JSONObject,bundle:String,mode:String,mapping:Map<String,String>):JSONObject {
  val base=JSONObject(draft.toString())
  val policy=base.remove("profileSelection")
  val out=JSONObject(Mobile.prepareConfigurationImport(base.toString(),bundle,JSONObject().put("mode",mode).put("mapping",JSONObject(mapping)).toString()))
  // This policy is opaque to the importer. Submit validates it against the
  // server's original and trims deleted IDs atomically, without adding grants.
  if(policy!=null)out.getJSONObject("candidate").put("profileSelection",policy)
  require(out.toString().toByteArray(Charsets.UTF_8).size<=1048576)
  return out
 }
}
