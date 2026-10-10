package ru.vpnc.quiclab

import android.app.Application
import android.os.Handler
import android.os.Looper

/** Jobs/providers can recreate the process without opening an Activity. */
class QuicLabApplication : Application() {
 override fun onCreate() {
  super.onCreate()
  // Run after component creation, not from a provider/Keystore initialization stack.
  // Restore saved intent only: active MDM and interrupted VPN from this OS boot.
  Handler(Looper.getMainLooper()).post {
   Diagnostics.init(this)
   runCatching{LocalConfigurationApply.recover(this)}
   MdmRuntime.restore(this)
   VpnProcessRecovery.restore(this)
  }
 }
}
