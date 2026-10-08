package ru.vpnc.quiclab

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.AtomicFile
import org.json.JSONObject
import java.io.File
import java.security.KeyStore
import java.util.concurrent.ConcurrentHashMap
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

internal data class MdmState(val binding:MdmBinding?,val active:Boolean,val localGeneration:Long,
 val rights:MdmRights,val cleanupPending:Boolean=false,val pendingEnrollment:Boolean=false,val appliedRevision:Long=0)

/** Separate from VPN identities/imports/backups. Corruption never silently unbinds a managed client. */
internal class MdmStore(private val file:File,private val alias:String="quic-lab-mdm-identity") {
 constructor(context:Context):this(File(context.noBackupFilesDir,"mdm-state.bin"))
 private val lock=locks.computeIfAbsent(file.absolutePath){Any()}
 private val cacheKey=file.absolutePath to alias
 private fun key(create:Boolean):SecretKey {
  val ks=KeyStore.getInstance("AndroidKeyStore").apply{load(null)}
  (ks.getKey(alias,null) as? SecretKey)?.let{return it}
  check(create){"Ключ хранилища MDM недоступен"}
  return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES,"AndroidKeyStore").apply{
   init(KeyGenParameterSpec.Builder(alias,KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT)
    .setBlockModes(KeyProperties.BLOCK_MODE_GCM).setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE).build())
  }.generateKey()
 }
 private fun load():JSONObject {
  if(!file.exists() && !File(file.path+".bak").exists())return JSONObject().put("schema",1).put("generation",0)
  val bytes=AtomicFile(file).openRead().use{input->
   val out=java.io.ByteArrayOutputStream();val buffer=ByteArray(8192)
   while(out.size()<=2*1024*1024){
    val n=input.read(buffer,0,minOf(buffer.size,2*1024*1024+1-out.size()))
    if(n<0)break
    out.write(buffer,0,n)
   }
   val raw=out.toByteArray();check(raw.size in 29..2*1024*1024){"Повреждено хранилище MDM"};raw
  }
  // Compare authenticated ciphertext, not timestamps: edits and corruption must be
  // visible even when another instance writes the same number of bytes immediately.
  val cached=synchronized(decoded){decoded[cacheKey]}
  if(cached!=null && cached.bytes.contentEquals(bytes))return JSONObject(cached.json)
  synchronized(decoded){decoded.remove(cacheKey)}
  val cipher=Cipher.getInstance("AES/GCM/NoPadding")
  cipher.init(Cipher.DECRYPT_MODE,key(false),GCMParameterSpec(128,bytes.copyOfRange(0,12)))
  val plain=cipher.doFinal(bytes,12,bytes.size-12)
  return try{JSONObject(String(plain,Charsets.UTF_8)).also{
   check(it.getInt("schema")==1 && it.getLong("generation")>=0){"Неизвестный формат MDM"}
   synchronized(decoded){decoded[cacheKey]=Decoded(bytes,it.toString())}
  }}finally{plain.fill(0)}
 }
 private fun save(j:JSONObject){
  val plain=j.toString().toByteArray(Charsets.UTF_8);require(plain.size<=2*1024*1024-28)
  val bytes=try{
   val cipher=Cipher.getInstance("AES/GCM/NoPadding");cipher.init(Cipher.ENCRYPT_MODE,key(true))
   cipher.iv+cipher.doFinal(plain)
  }finally{plain.fill(0)}
  val atomic=AtomicFile(file);val out=atomic.startWrite()
  try{out.write(bytes);atomic.finishWrite(out)}catch(e:Exception){atomic.failWrite(out);throw e}
 }
 fun read():MdmState=synchronized(lock){state(load())}
 internal fun document():JSONObject=synchronized(lock){load()}
 fun manualTelemetry():Boolean=synchronized(lock){load().optBoolean("radio_manual")}
 fun setManualTelemetry(enabled:Boolean){edit{j->
  if(enabled)check(MdmTelemetryPolicy.consented(state(j))){"Включите MDM и разрешите отправку данных"}
  j.put("radio_manual",enabled)
 }}
 internal fun secret():String=synchronized(lock){load().optString("secret")}
 internal fun edit(change:(JSONObject)->Unit):MdmState=synchronized(lock){
  val j=load();change(j);val result=state(j);save(j);result
 }
 fun erase():MdmState=synchronized(lock){
  val j=JSONObject().put("schema",1).put("generation",Math.addExact(load().getLong("generation"),1))
  save(j);state(j)
 }
 companion object{
  private val locks=ConcurrentHashMap<String,Any>()
  private data class Decoded(val bytes:ByteArray,val json:String)
  // Detached JSON on every read; never retain a mutable document returned to callers.
  private val decoded=object:LinkedHashMap<Pair<String,String>,Decoded>(8,0.75f,true){
   override fun removeEldestEntry(eldest:MutableMap.MutableEntry<Pair<String,String>,Decoded>?)=size>8
  }
  internal fun bindingJson(b:MdmBinding)=JSONObject().put("id",b.id).put("endpoint",b.endpoint).put("epoch",b.epoch)
   .put("requestedRights",b.requestedRights.json()).put("active",b.active)
  private fun state(j:JSONObject):MdmState {
   val b=j.optJSONObject("binding")?.let{
    MdmBinding(it.getString("id"),it.getString("endpoint"),it.getLong("epoch"),
     MdmRights.parse(it.getJSONObject("requestedRights")),it.optBoolean("active"))
   }
   return MdmState(b,j.optBoolean("active") && b!=null,j.getLong("generation"),
    MdmRights.parse(j.optJSONObject("rights")?:JSONObject()),j.optBoolean("cleanupPending") || j.optBoolean("configCleanupPending"),
    j.has("pending"),j.optLong("appliedRevision"))
  }
 }
}
