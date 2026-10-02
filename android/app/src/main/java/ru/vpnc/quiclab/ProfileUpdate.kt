package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.content.pm.PackageManager
import android.os.Build
import androidx.core.content.FileProvider
import java.io.File
import java.security.MessageDigest
import java.util.UUID
import org.json.JSONObject

internal class VpnProfileUpdateRuntime(val dnsExitId:String,val budget:mobile.TrafficBudget,val tunToken:Any,private val restart:(String)->Unit) {
 fun restartExit(id:String)=restart(id)
}

/** Explicit updates use the trusted physical-network C3 coordinator. No polling. */
internal object ProfileUpdate {
 private val localFields=setOf("routes","mode","apps","global_apps","transport","max_availability",
  "bond_copy_budget","bond_cell_budget","bond_copy_kib","cell_mib","priority","enabled")
 private val privateFields=setOf("key","certificate","awg_config","update_token")
 fun fetch(context:Context,exitId:String):JSONObject=fetch(context,exitId,false)
 fun fetch(context:Context,exitId:String,allowOverBudget:Boolean):JSONObject {
  val file=File(context.cacheDir,"update-config-${UUID.randomUUID()}.json")
  try {
   LabVpnService.fetchUpdate(context,exitId,"config",file,allowOverBudget)
   require(file.length() in 1..1048576) {"Конфигурация слишком большая"}
   val candidate=JSONObject(file.readText(Charsets.UTF_8))
   val identity=VpnIdentity.load(context,exitId)
   validateCandidate(identity.optJSONObject("update_envelope"),candidate,identity.getString("device_id"))
   return candidate
  } finally {file.delete()}
 }
 fun validateCandidate(current:JSONObject?,incoming:JSONObject,deviceID:String) {
  require(incoming.getInt("schema_version")==1) {"Неподдерживаемая версия конфигурации"}
  require(incoming.getString("device_id")==deviceID && incoming.getString("exit_id")==deviceID) {"Конфигурация другого устройства"}
  val revision=incoming.getLong("config_revision")
  require(revision>0 && revision>=(current?.optLong("config_revision",0) ?: 0)) {"Сервер прислал устаревшую конфигурацию"}
  val server=incoming.getJSONObject("server_config")
  require(localFields.none {server.has(it)}) {"Сервер не должен менять локальные предпочтения"}
  require(!server.has("update_token")) {"Ключ обновления нельзя менять конфигурацией"}
  val validation=JSONObject(server.toString());validation.remove("config_url");validation.remove("apk_url")
  for(k in listOf("config_url","apk_url")) if(server.has(k)) ServiceTransfer.validateMetadata(JSONObject().put("apk_url",server.getString(k)))
  require(ProfileImport.validate(validation)=="vpn") {"Нужен профиль VPN"}
  val caps=incoming.getJSONObject("capabilities")
  require(caps.getInt("control_version")>=1 && caps.getInt("data_version")>=1 && caps.getInt("min_android_version_code")>=0) {"Некорректные возможности сервера"}
  if(current!=null && revision==current.optLong("config_revision")) {
   require(canonical(current)==canonical(incoming)) {"Одна ревизия содержит разные настройки"}
  }
  val hash=caps.optString("apk_sha256")
  require(hash.isEmpty() || hash.matches(Regex("[a-fA-F0-9]{64}"))) {"Некорректный hash APK"}
  if(caps.optString("apk_url").isNotEmpty()) ServiceTransfer.validateMetadata(JSONObject().put("apk_url",caps.getString("apk_url")))
 }
 private fun canonical(value:Any?):String=when(value) {
  is JSONObject -> value.keys().asSequence().toList().sorted().joinToString(prefix="{",postfix="}") {JSONObject.quote(it)+":"+canonical(value.get(it))}
  is org.json.JSONArray -> (0 until value.length()).joinToString(prefix="[",postfix="]") {canonical(value.get(it))}
  else -> org.json.JSONArray().put(value).toString().let {it.substring(1,it.length-1)}
 }
 fun diff(current:JSONObject,incoming:JSONObject):JSONObject {
  val result=JSONObject();val a=current.optJSONObject("server_config") ?: current
  val b=incoming.getJSONObject("server_config")
  val keys=(a.keys().asSequence().toSet()+b.keys().asSequence().toSet()).sorted()
  for(k in keys) if(canonical(a.opt(k))!=canonical(b.opt(k))) {
   if(k in privateFields) result.put(k,"Учётные данные изменились")
   else result.put(k,JSONObject().put("before",a.opt(k) ?: JSONObject.NULL).put("after",b.opt(k) ?: JSONObject.NULL))
  }
  if(canonical(current.optJSONObject("capabilities"))!=canonical(incoming.optJSONObject("capabilities"))) result.put("capabilities","Требования версии/обновления изменились")
  return result
 }
 fun compatible(envelope:JSONObject):Boolean {
  val caps=envelope.getJSONObject("capabilities")
  return caps.getInt("control_version")==1 && caps.getInt("data_version")==1 && caps.getInt("min_android_version_code")<=BuildConfig.VERSION_CODE
 }
 @Synchronized fun apply(context:Context,exitId:String,incoming:JSONObject) {
  val existing=VpnIdentity.load(context,exitId)
  validateCandidate(existing.optJSONObject("update_envelope"),incoming,existing.getString("device_id"))
  require(compatible(incoming)) {"Сначала обновите приложение"}
  val server=incoming.getJSONObject("server_config")
  val currentDNS=VpnProfiles.preferences(context,exitId).getString("dns","1.1.1.1")
  require(!LabVpnService.isActiveDnsExit(context,exitId) || server.optString("dns","1.1.1.1")==currentDNS) {
   "Для изменения DNS остановите VPN и повторите обновление"
  }
  validateKeys(server)
  val updated=JSONObject(existing.toString())
  for(k in listOf("certificate","key","awg_config")) {updated.remove(k);if(server.has(k)) updated.put(k,server.get(k))}
  updated.put("server_config",JSONObject(server.toString())).put("update_envelope",JSONObject(incoming.toString()))
  for(k in listOf("config_url","apk_url")) {if(server.has(k)) updated.put(k,server.getString(k))}
  val caps=incoming.getJSONObject("capabilities")
  if(caps.has("apk_url")) updated.put("apk_url",caps.getString("apk_url"))
  // Sole commit point: keys, revision and server-owned fields in one encrypted file.
  VpnIdentity.writeBundle(context,exitId,updated)
  LabVpnService.restartUpdatedExit(context,exitId)
 }
 private fun validateKeys(server:JSONObject) {
  if(server.has("awg_config")) mobile.Mobile.validateAWGConfig(server.getString("awg_config"))
  if(!server.has("certificate")) {require(server.has("awg_config"));return}
  val cert=java.security.cert.CertificateFactory.getInstance("X.509")
   .generateCertificate(server.getString("certificate").byteInputStream()) as java.security.cert.X509Certificate
  cert.checkValidity();require(cert.extendedKeyUsage?.contains("1.3.6.1.5.5.7.3.2")==true) {"Нужен клиентский сертификат TLS"}
  val raw=server.getString("key").replace("-----BEGIN PRIVATE KEY-----","").replace("-----END PRIVATE KEY-----","").replace(Regex("\\s"),"")
  val key=java.security.KeyFactory.getInstance("EC").generatePrivate(java.security.spec.PKCS8EncodedKeySpec(android.util.Base64.decode(raw,android.util.Base64.DEFAULT)))
  val challenge=ByteArray(32).also {java.security.SecureRandom().nextBytes(it)}
  val signature=java.security.Signature.getInstance("SHA256withECDSA")
  signature.initSign(key);signature.update(challenge);val signed=signature.sign()
  signature.initVerify(cert.publicKey);signature.update(challenge);require(signature.verify(signed)) {"Ключ не соответствует сертификату"}
 }
 fun validateApkMetadata(expectedHash:String,actualHash:String,actualPackage:String,expectedPackage:String,actualSigners:Set<String>,expectedSigners:Set<String>) {
  require(expectedHash.matches(Regex("[a-fA-F0-9]{64}")) && expectedHash.equals(actualHash,true)) {"APK hash не совпадает"}
  require(actualPackage==expectedPackage) {"APK другого приложения"}
  require(expectedSigners.isNotEmpty() && actualSigners==expectedSigners) {"Подпись APK не совпадает"}
 }
 @Suppress("DEPRECATION") private fun signers(info:android.content.pm.PackageInfo):Set<String> {
  val certs=if(Build.VERSION.SDK_INT>=28) info.signingInfo?.apkContentsSigners else info.signatures
  return certs?.map {digest(it.toByteArray())}?.toSet().orEmpty()
 }
 private fun digest(bytes:ByteArray)=MessageDigest.getInstance("SHA-256").digest(bytes).joinToString("") {"%02x".format(it.toInt() and 255)}
 @Suppress("DEPRECATION") fun validateApk(context:Context,file:File,expectedHash:String) {
  require(file.length() in 1..268435456) {"Некорректный размер APK"}
  val hasher=MessageDigest.getInstance("SHA-256")
  file.inputStream().use {input->val b=ByteArray(65536);while(true) {val n=input.read(b);if(n<0)break;hasher.update(b,0,n)}}
  val hash=hasher.digest().joinToString("") {"%02x".format(it.toInt() and 255)}
  require(expectedHash.equals(hash,true)) {"APK hash не совпадает"}
  val flags=if(Build.VERSION.SDK_INT>=28) PackageManager.GET_SIGNING_CERTIFICATES else PackageManager.GET_SIGNATURES
  val archive=requireNotNull(context.packageManager.getPackageArchiveInfo(file.absolutePath,flags)) {"APK не читается"}
  val installed=context.packageManager.getPackageInfo(context.packageName,flags)
  validateApkMetadata(expectedHash,hash,archive.packageName,context.packageName,signers(archive),signers(installed))
 }
 fun downloadApk(context:Context,exitId:String,envelope:JSONObject,allowOverBudget:Boolean=false):File {
  val identity=VpnIdentity.load(context,exitId)
  validateCandidate(identity.optJSONObject("update_envelope"),envelope,identity.getString("device_id"))
  val caps=envelope.getJSONObject("capabilities")
  val file=File(File(context.cacheDir,"updates").apply {check(mkdirs() || isDirectory)},"quic-lab-${UUID.randomUUID()}.apk")
  try {
   // URL can be newly learned from this authenticated fetch; preserve current
   // profile until apply by passing approved metadata as an ephemeral override.
   LabVpnService.fetchUpdate(context,exitId,"apk",file,allowOverBudget,caps.getString("apk_url"))
   validateApk(context,file,caps.getString("apk_sha256"));return file
  } catch(e:Exception) {file.delete();throw e}
 }
 fun installerIntent(context:Context,file:File,expectedHash:String):Intent {
  validateApk(context,file,expectedHash)
  val uri=FileProvider.getUriForFile(context,context.packageName+".diagnostics",file)
  return Intent(Intent.ACTION_VIEW).setDataAndType(uri,"application/vnd.android.package-archive")
   .addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
 }
}
