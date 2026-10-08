package ru.vpnc.quiclab

import android.Manifest
import android.app.Activity
import android.content.Intent
import android.os.Bundle
import android.widget.*

/** System permissions and vendor background settings, independent of VPN configuration. */
class PermissionsActivity:Activity(){
 override fun onCreate(savedInstanceState:Bundle?){
  super.onCreate(savedInstanceState)
  val ui=ClientUi(this);val root=ui.column()
  root.addView(ui.button("Назад"){finish()})
  val panel=ui.column().apply{setPadding(ui.dp(16),ui.dp(8),ui.dp(16),ui.dp(16))}
  root.addView(ScrollView(this).apply{addView(panel)},LinearLayout.LayoutParams(-1,0,1f))
  panel.addView(ui.text("Разрешения и фоновая работа",22f,true))
  var box=ui.card(panel)
  box.addView(ui.text("Сведения о сети",18f,true))
  box.addView(ui.text("Для отображения имени Wi-Fi, точки доступа и данных соты Android требует разрешение геопозиции. Передача этих данных через MDM включается отдельно."))
  ui.add(box,ui.button("Разрешить сведения о сети"){
   requestPermissions(arrayOf(Manifest.permission.ACCESS_COARSE_LOCATION,Manifest.permission.ACCESS_FINE_LOCATION),21)
  })
  box=ui.card(panel)
  box.addView(ui.text("Фоновая работа VPN",18f,true))
  box.addView(ui.text("На Xiaomi/MIUI разрешите автозапуск приложения в системных настройках. Очистка памяти может завершить VPN даже с постоянным уведомлением. При необходимости закрепите приложение в недавних и снимите ограничения батареи. Разрешение автозапуска не включает VPN после перезагрузки: подключение запускаете вы."))
  ui.add(box,ui.button("Системные настройки приложения"){
   runCatching{startActivity(Intent(android.provider.Settings.ACTION_APPLICATION_DETAILS_SETTINGS,android.net.Uri.parse("package:$packageName")))}
    .onFailure{Toast.makeText(this,"Откройте настройки Android → Приложения → QUIC Lab",Toast.LENGTH_LONG).show()}
  })
  ui.install(root)
 }
}
