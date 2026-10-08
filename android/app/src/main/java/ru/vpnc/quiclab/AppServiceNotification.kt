package ru.vpnc.quiclab

import android.app.*
import android.content.Context
import android.content.Intent

/** All foreground services share one visible notification, but keep their own FGS types. */
internal object AppServiceNotification {
 const val ID = 42
 private val owners = linkedMapOf<Service, String>()
 private var vpnContent: (() -> VpnNotificationContent)? = null

 @Synchronized fun start(service: Service, kind: String, type: Int = 0, content: (() -> VpnNotificationContent)? = null) {
  owners[service] = kind
  if(kind == "vpn") vpnContent = content
  try {
   val notice = build(service)
   if(type == 0) service.startForeground(ID, notice) else service.startForeground(ID, notice, type)
  } catch(e: Exception) {
   owners.remove(service)
   if(kind == "vpn") vpnContent = null
   refresh(service)
   throw e
  }
 }

 @Synchronized fun stop(service: Service) {
  val kind = owners.remove(service) ?: return
  if(kind == "vpn") vpnContent = null
  // Detach prevents one stopping service from removing the other services' notification.
  service.stopForeground(if(owners.isEmpty()) Service.STOP_FOREGROUND_REMOVE else Service.STOP_FOREGROUND_DETACH)
  refresh(service)
 }

 @Synchronized fun refresh(context: Context) {
  if(owners.isNotEmpty()) context.getSystemService(NotificationManager::class.java).notify(ID, build(context))
 }

 internal fun title(base: String, mdm: Boolean, geo: Boolean): String =
  (if(mdm) "[MDM]" else "") + (if(geo) "[GEO]" else "") +
   (if(mdm || geo) " " else "") + base

 private fun build(context: Context): Notification {
  context.getSystemService(NotificationManager::class.java).createNotificationChannel(
   NotificationChannel("vpn", "QUIC Lab", NotificationManager.IMPORTANCE_LOW))
  val content = vpnContent?.invoke() ?: VpnNotificationContent("VPN выключен", "Управление — в настройках приложения", "Управление — в настройках приложения")
  val open = PendingIntent.getActivity(context, 2, Intent(context, VpnActivity::class.java), PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE)
  return Notification.Builder(context, "vpn")
   .setSmallIcon(android.R.drawable.ic_lock_lock)
   .setContentTitle(title(content.title, owners.containsValue("mdm"), owners.containsValue("geo")))
   .setContentText(content.text).setStyle(Notification.BigTextStyle().bigText(content.details))
   .setContentIntent(open).setOnlyAlertOnce(true).setShowWhen(false).setOngoing(true).build()
 }
}
