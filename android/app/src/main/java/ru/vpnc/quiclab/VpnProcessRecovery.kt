package ru.vpnc.quiclab

import android.content.Context
import android.content.Intent
import android.net.VpnService
import org.json.JSONObject

/** Resume a previously running VPN in the same boot, never a fresh session. */
internal object VpnProcessRecovery {
 fun request(run:VpnBudgetRun,active:Boolean,consented:Boolean,start:()->Unit) {
  if(!active && consented && run.shouldResume())start()
 }
 fun restore(context:Context) {
  runCatching {
   request(VpnBudgetRun.sharedForContext(context),LabVpnService.active,VpnService.prepare(context)==null) {
    context.startForegroundService(Intent(context,LabVpnService::class.java).putExtra("saved_run_restart",true))
   }
  }.onFailure {
   Diagnostics.event("vpn",JSONObject().put("event","restore_denied").put("error",it.javaClass.simpleName))
  }
 }
}
