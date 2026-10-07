package ru.vpnc.quiclab

import android.content.Context
import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.io.File
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec
import org.json.JSONObject

internal object VpnIdentity {
    private const val ALIAS = "quic-lab-vpn-identity"

    private fun key(): SecretKey {
        val store = KeyStore.getInstance("AndroidKeyStore").apply { load(null) }
        (store.getKey(ALIAS, null) as? SecretKey)?.let {
            return it
        }
        return KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, "AndroidKeyStore")
            .apply {
                init(
                    KeyGenParameterSpec.Builder(
                            ALIAS,
                            KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
                        )
                        .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                        .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                        .build()
                )
            }
            .generateKey()
    }

    private fun pem(type: String, b: ByteArray) =
        "-----BEGIN $type-----\n" +
            Base64.encodeToString(b, Base64.NO_WRAP).chunked(64).joinToString("\n") +
            "\n-----END $type-----\n"

    private fun requireManualImport(context: Context) {
        val id=VpnProfiles.current(context).id
        val local=VpnProfiles.preferences(context,id)
        require(!local.getBoolean("managed_profile",false)) {"Сертификат управляемого профиля задаёт сервер; создайте отдельный профиль"}
        check(!LabVpnService.active) {"Сначала остановите VPN"}
    }
    fun import(context: Context, bytes: ByteArray, password: CharArray): String {
        requireManualImport(context)
        val bundle = parsePkcs12(bytes, password)
        writeBundle(context, VpnProfiles.current(context).id, bundle)
        return bundle.getString("subject")
    }

    fun parsePkcs12(bytes: ByteArray, password: CharArray): JSONObject = try {
        val store = KeyStore.getInstance("PKCS12").apply { load(bytes.inputStream(), password) }
        val aliases = store.aliases().toList().filter { store.isKeyEntry(it) }
        require(aliases.size == 1) { "PKCS#12 должен содержать ровно один ключ" }
        val alias = aliases.single()
        val chain = store.getCertificateChain(alias)
        val cert = chain[0] as java.security.cert.X509Certificate
        cert.checkValidity()
        val content =
            JSONObject()
                .put("certificate", chain.joinToString("") { pem("CERTIFICATE", it.encoded) })
                .put("key", pem("PRIVATE KEY", store.getKey(alias, password).encoded))
                .put("subject", cert.subjectX500Principal.name)
                .toString()
                .toByteArray()
        JSONObject(String(content, Charsets.UTF_8)).also { content.fill(0) }
        } finally { password.fill('\u0000') }

    private fun save(context: Context, content: ByteArray) {
        try { writeBundle(context, VpnProfiles.current(context).id, JSONObject(String(content))) }
        finally { content.fill(0) }
    }

    @Synchronized fun writeBundle(context: Context, id: String, bundle: JSONObject) {
        val content = bundle.toString().toByteArray(Charsets.UTF_8)
        try {
            val cipher = Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
            val encrypted = cipher.iv + cipher.doFinal(content)
            val destination = android.util.AtomicFile(VpnProfiles.identityFile(context, id))
            val out = destination.startWrite()
            try { out.write(encrypted); destination.finishWrite(out) }
            catch (failure: Exception) { destination.failWrite(out); throw failure }
        } finally { content.fill(0) }
    }
    fun importProfile(context: Context, profile: JSONObject): String {
        requireManualImport(context)
        val content = certificateBundle(profile)
        save(context, content.toString().toByteArray(Charsets.UTF_8))
        return content.getString("subject")
    }

    private fun certificateBundle(profile: JSONObject): JSONObject {
        val certificate = profile.getString("certificate")
        val privateKey = profile.getString("key")
        val cert = java.security.cert.CertificateFactory.getInstance("X.509")
            .generateCertificate(certificate.byteInputStream()) as java.security.cert.X509Certificate
        cert.checkValidity()
        require(cert.extendedKeyUsage?.contains("1.3.6.1.5.5.7.3.2") == true) { "Нужен клиентский сертификат TLS" }
        val rawKey = privateKey.replace("-----BEGIN PRIVATE KEY-----", "").replace("-----END PRIVATE KEY-----", "").replace(Regex("\\s"), "")
        val key = java.security.KeyFactory.getInstance("EC").generatePrivate(java.security.spec.PKCS8EncodedKeySpec(Base64.decode(rawKey, Base64.DEFAULT)))
        val challenge = ByteArray(32).also { java.security.SecureRandom().nextBytes(it) }
        val signer = java.security.Signature.getInstance("SHA256withECDSA")
        signer.initSign(key); signer.update(challenge); val signature = signer.sign()
        signer.initVerify(cert.publicKey); signer.update(challenge)
        require(signer.verify(signature)) { "Ключ не соответствует сертификату" }
        return JSONObject().put("certificate",certificate).put("key",privateKey)
            .put("subject",cert.subjectX500Principal.name)
    }

    fun importBundle(context: Context, profile: JSONObject) {
        requireManualImport(context)
        save(context, bundleFromProfile(profile).toString().toByteArray())
    }

    internal fun bundleFromProfile(profile: JSONObject): JSONObject {
        require(ProfileImport.validate(profile) == "vpn") { "Нужен профиль VPN" }
        val content = if (profile.has("certificate")) certificateBundle(profile)
            else JSONObject().put("subject", if (profile.has("vless_uri")) "VLESS" else "AmneziaWG")
        ProfileImport.vlessConfig(profile)?.let { content.put("vless_config", it) }
        if (profile.has("awg_config")) {
            val raw = profile.getString("awg_config")
            mobile.Mobile.validateAWGConfig(raw)
            content.put("awg_config", raw)
        }
        for (field in listOf("update_token", "device_id", "config_url", "apk_url")) {
            if (profile.has(field)) content.put(field, profile.getString(field))
        }
        if (profile.optString("config_url").isNotEmpty()) {
 val server=JSONObject(profile.toString());for(k in listOf("update_token","device_id","mode","routes")) server.remove(k)
 content.put("server_config",server)
 }
 require(content.has("certificate") || content.has("awg_config") || content.has("vless_config")) { "В профиле нет ключей" }
        return content
    }

    fun importAWG(context: Context, raw: String) {
        requireManualImport(context)
        mobile.Mobile.validateAWGConfig(raw)
        save(context, JSONObject().put("awg_config", raw).put("subject", "AmneziaWG").toString().toByteArray())
    }

    /** Canonical imported profile contains UUID/REALITY key; encrypted storage only. */
    fun importVLESS(context: Context, raw: String) {
        requireManualImport(context)
        val canonical = mobile.Mobile.importVLESSConfig(raw)
        save(context, JSONObject().put("vless_config", canonical).put("subject", "VLESS").toString().toByteArray(Charsets.UTF_8))
    }
    @Synchronized fun load(context: Context, id: String = VpnProfiles.current(context).id): JSONObject {
        val bytes = android.util.AtomicFile(VpnProfiles.identityFile(context, id)).readFully()
        val cipher =
            Cipher.getInstance("AES/GCM/NoPadding").apply {
                init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
            }
        return JSONObject(String(cipher.doFinal(bytes.copyOfRange(12, bytes.size))))
    }
}
