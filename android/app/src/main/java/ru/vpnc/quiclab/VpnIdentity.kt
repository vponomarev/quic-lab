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

    fun import(context: Context, bytes: ByteArray, password: CharArray): String {
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
        val cipher =
            Cipher.getInstance("AES/GCM/NoPadding").apply { init(Cipher.ENCRYPT_MODE, key()) }
        val encrypted = cipher.iv + cipher.doFinal(content)
        val temp = File(context.filesDir, "vpn-identity.tmp")
        temp.writeBytes(encrypted)
        check(temp.renameTo(File(context.filesDir, "vpn-identity.enc"))) {
            "Не удалось сохранить сертификат"
        }
        content.fill(0)
        password.fill('\u0000')
        return cert.subjectX500Principal.name
    }

    fun load(context: Context): JSONObject {
        val bytes = File(context.filesDir, "vpn-identity.enc").readBytes()
        val cipher =
            Cipher.getInstance("AES/GCM/NoPadding").apply {
                init(Cipher.DECRYPT_MODE, key(), GCMParameterSpec(128, bytes.copyOfRange(0, 12)))
            }
        return JSONObject(String(cipher.doFinal(bytes.copyOfRange(12, bytes.size))))
    }
}
