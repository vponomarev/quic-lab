package ru.vpnc.quiclab

import android.app.Application
import android.os.Handler
import android.os.Looper

/** Jobs/providers can recreate the process without opening an Activity. */
class QuicLabApplication : Application() {
 override fun onCreate() {
  super.onCreate()
  registerActivityLifecycleCallbacks(object:ActivityLifecycleCallbacks{
   private var started=0
   override fun onActivityStarted(activity:android.app.Activity){
    started++;MdmBackground.visible=true
    if(started==1)MdmRuntime.restore(this@QuicLabApplication,foreground=true)
   }
   override fun onActivityStopped(activity:android.app.Activity){started=(started-1).coerceAtLeast(0);MdmBackground.visible=started>0}
   override fun onActivityCreated(a:android.app.Activity,b:android.os.Bundle?){}
   override fun onActivityResumed(a:android.app.Activity){}
   override fun onActivityPaused(a:android.app.Activity){}
   override fun onActivitySaveInstanceState(a:android.app.Activity,b:android.os.Bundle){}
   override fun onActivityDestroyed(a:android.app.Activity){}
  })
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
