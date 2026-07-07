package com.agentmaster.app

import android.content.Context
import android.content.SharedPreferences
import androidx.security.crypto.EncryptedSharedPreferences
import androidx.security.crypto.MasterKey
import com.agentmaster.core.AmJson
import com.agentmaster.core.MACHINES_STORAGE_KEY
import com.agentmaster.core.MachineStore
import com.agentmaster.core.PersistedMachines
import kotlinx.coroutines.Dispatchers
import kotlinx.coroutines.withContext

/**
 * [MachineStore] backed by AndroidX EncryptedSharedPreferences. Machine tokens
 * are secrets, so they are stored in an AES256-encrypted prefs file keyed by a
 * hardware-backed (StrongBox where available) master key.
 *
 * This is the Android analogue of the web app's localStorage store and the
 * desktop app's OS secure store (see frontend/apps/web/src/storage.ts).
 */
class EncryptedMachineStore(context: Context) : MachineStore {

    private val prefs: SharedPreferences by lazy {
        val appContext = context.applicationContext
        val masterKey = MasterKey.Builder(appContext)
            .setKeyScheme(MasterKey.KeyScheme.AES256_GCM)
            .build()
        EncryptedSharedPreferences.create(
            appContext,
            PREFS_FILE,
            masterKey,
            EncryptedSharedPreferences.PrefKeyEncryptionScheme.AES256_SIV,
            EncryptedSharedPreferences.PrefValueEncryptionScheme.AES256_GCM,
        )
    }

    override suspend fun load(): PersistedMachines = withContext(Dispatchers.IO) {
        val raw = prefs.getString(MACHINES_STORAGE_KEY, null) ?: return@withContext PersistedMachines()
        try {
            AmJson.decodeFromString(PersistedMachines.serializer(), raw)
        } catch (_: Exception) {
            // Malformed storage — start clean rather than crash.
            PersistedMachines()
        }
    }

    override suspend fun save(state: PersistedMachines) = withContext(Dispatchers.IO) {
        val raw = AmJson.encodeToString(PersistedMachines.serializer(), state)
        prefs.edit().putString(MACHINES_STORAGE_KEY, raw).apply()
    }

    private companion object {
        const val PREFS_FILE = "agent_master_machines"
    }
}
