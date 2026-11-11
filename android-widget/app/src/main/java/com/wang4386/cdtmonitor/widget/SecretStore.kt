package com.wang4386.cdtmonitor.widget

import android.security.keystore.KeyGenParameterSpec
import android.security.keystore.KeyProperties
import android.util.Base64
import java.nio.charset.StandardCharsets
import java.security.KeyStore
import javax.crypto.Cipher
import javax.crypto.KeyGenerator
import javax.crypto.SecretKey
import javax.crypto.spec.GCMParameterSpec

/**
 * Encrypts the API key with an Android Keystore AES/GCM key.
 *
 * The stored format ("base64(iv):base64(ciphertext)") is byte-compatible with
 * the previous Java implementation, so existing installs keep their saved key
 * after upgrading to the Kotlin version.
 */
object SecretStore {
    private const val KEY_ALIAS = "cdt-monitor-widget-api-key"
    private const val ANDROID_KEYSTORE = "AndroidKeyStore"
    private const val CIPHER = "AES/GCM/NoPadding"

    fun encrypt(plainText: String?): String = encrypt(plainText, KEY_ALIAS)

    fun encrypt(plainText: String?, alias: String): String {
        if (plainText.isNullOrEmpty()) return ""
        return try {
            val cipher = Cipher.getInstance(CIPHER)
            cipher.init(Cipher.ENCRYPT_MODE, getKey(alias))
            val iv = Base64.encodeToString(cipher.iv, Base64.NO_WRAP)
            val data = Base64.encodeToString(
                cipher.doFinal(plainText.toByteArray(StandardCharsets.UTF_8)),
                Base64.NO_WRAP,
            )
            "$iv:$data"
        } catch (ignored: Exception) {
            ""
        }
    }

    fun decrypt(encrypted: String?): String = decrypt(encrypted, KEY_ALIAS)

    fun decrypt(encrypted: String?, alias: String): String {
        if (encrypted.isNullOrEmpty()) return ""
        return try {
            val parts = encrypted.split(":", limit = 2)
            if (parts.size != 2) return ""
            val iv = Base64.decode(parts[0], Base64.NO_WRAP)
            val data = Base64.decode(parts[1], Base64.NO_WRAP)
            val cipher = Cipher.getInstance(CIPHER)
            cipher.init(Cipher.DECRYPT_MODE, getKey(alias), GCMParameterSpec(128, iv))
            String(cipher.doFinal(data), StandardCharsets.UTF_8)
        } catch (ignored: Exception) {
            ""
        }
    }

    private fun getKey(alias: String): SecretKey {
        val keyStore = KeyStore.getInstance(ANDROID_KEYSTORE)
        keyStore.load(null)
        if (keyStore.containsAlias(alias)) {
            return (keyStore.getEntry(alias, null) as KeyStore.SecretKeyEntry).secretKey
        }
        val generator = KeyGenerator.getInstance(KeyProperties.KEY_ALGORITHM_AES, ANDROID_KEYSTORE)
        generator.init(
            KeyGenParameterSpec.Builder(
                alias,
                KeyProperties.PURPOSE_ENCRYPT or KeyProperties.PURPOSE_DECRYPT,
            )
                .setBlockModes(KeyProperties.BLOCK_MODE_GCM)
                .setEncryptionPaddings(KeyProperties.ENCRYPTION_PADDING_NONE)
                .build(),
        )
        return generator.generateKey()
    }
}
