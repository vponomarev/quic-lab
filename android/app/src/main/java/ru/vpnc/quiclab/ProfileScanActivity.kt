package ru.vpnc.quiclab
import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.widget.TextView
import com.google.zxing.integration.android.IntentIntegrator
import org.json.JSONObject

class ProfileScanActivity : Activity() {
    private var enrollmentURL:String? = null
    override fun onCreate(savedInstanceState:Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(TextView(this).apply{text="Импорт профиля QUIC Lab…";setPadding(24,72,24,24)})
        enrollmentURL=savedInstanceState?.getString("enrollment_url")
        val supplied=intent.getStringExtra("import_text")
        if(supplied!=null) { importText(supplied.trim());return }
        if(savedInstanceState==null) IntentIntegrator(this).setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            .setPrompt("QR QUIC Lab, AmneziaWG или VLESS").setBeepEnabled(false).setOrientationLocked(false).initiateScan()
    }
    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode:Int,resultCode:Int,data:Intent?) {
        val scan=IntentIntegrator.parseActivityResult(requestCode,resultCode,data)
        if(scan==null){super.onActivityResult(requestCode,resultCode,data);return}
        val raw=scan.contents ?: run{finish();return}
        importText(raw.trim())
    }
    private fun importText(raw:String) {
        try {
            check(!LabVpnService.active){"Сначала остановите VPN"}
            require(raw.length<=32768){"Конфигурация слишком большая"}
            if(raw.trimStart().startsWith("vless://")) {
                VlessImport.review(this,raw,{setResult(RESULT_OK,Intent().putExtra("kind","vpn"));finish()},{finish()})
            }
            else if(raw.trimStart().startsWith("[Interface]")) {
                AwgImport.review(this,raw,{setResult(RESULT_OK,Intent().putExtra("kind","vpn"));finish()},{finish()})
            }
            else if(raw.trimStart().startsWith("{")) {
                val profile = JSONObject(raw)
                if(profile.optString("protocol") == "vless") VlessImport.review(this,raw,{setResult(RESULT_OK,Intent().putExtra("kind","vpn"));finish()},{finish()})
                else review(profile)
            }
            else {val uri=ProfileImport.enrollment(raw)
                enrollmentURL=raw
                val debug = uri.path.endsWith("/capture/enroll")
                AlertDialog.Builder(this).setTitle(if(debug) "Получить настройки отладки?" else "Получить VPN-профиль?")
                    .setMessage(if(debug) "Сервер: ${uri.host}\nБудут получены настройки учебного захвата. Экспорт TLS secrets включается отдельным подтверждением." else "Сервер: ${uri.host}\nБудут получены настройки, клиентский сертификат и закрытый ключ.")
                    .setNegativeButton("Отмена"){_,_->finish()}.setPositiveButton("Получить"){_,_->
                        Thread{try{val p=ProfileImport.fetch(this,raw);runOnUiThread{if(!isFinishing)try{review(p)}catch(_:Exception){fail(IllegalArgumentException("Проверьте формат и параметры профиля"))}}}catch(e:Exception){runOnUiThread{fail(e)}}}.start()
                    }.show()
            }
        }catch(_:Exception){fail(IllegalArgumentException("Проверьте формат и параметры профиля"))}
    }
    private fun review(p:JSONObject){val kind=ProfileImport.validate(p)
        if(kind=="capture") {
            AlertDialog.Builder(this).setTitle("Учебный захват Wireshark")
                .setMessage("${p.getString("capture_kind").uppercase()} · ${p.getString("hostname")}\nTLS secrets новых соединений будут отправляться на ${java.net.URI(p.getString("capture_url")).host} до окончания сессии (не более 10 минут). Это позволит преподавателю расшифровать учебный трафик. Адреса и VPN-профили не меняются.")
                .setNegativeButton("Отмена"){_,_->finish()}.setPositiveButton("Включить"){_,_->try { ProfileImport.save(this,p);setResult(RESULT_OK,Intent().putExtra("kind","capture"));finish() }catch(e:Exception){fail(e)}}.show()
            return
        }
        val address=if(kind=="echo")p.getString("endpoint") else ProfileImport.reviewAddress(p)
        AlertDialog.Builder(this).setTitle(if(kind=="echo")"Настройки echo" else "VPN: ${p.optString("name")}")
            .setMessage("$address\nTLS: ${p.getString("hostname")}\n${if(kind=="vpn") "Добавить новый VPN-профиль?" else "Заменить настройки echo?"}")
            .setNegativeButton("Отмена"){_,_->finish()}.setPositiveButton("Сохранить"){_,_->try{
                val saved=ProfileImport.save(this,p);enrollmentURL?.let { ProfileImport.completeEnrollment(this,it) };setResult(RESULT_OK,Intent().putExtra("kind",saved));finish()
            }catch(_:Exception){fail(IllegalArgumentException("Проверьте формат и параметры профиля"))}}.show()
    }
    override fun onSaveInstanceState(out:Bundle){out.putString("enrollment_url",enrollmentURL);super.onSaveInstanceState(out)}
    private fun fail(e:Exception){if(isFinishing)return;AlertDialog.Builder(this).setTitle("Импорт не выполнен").setMessage(e.message ?: "Ошибка профиля").setPositiveButton("OK"){_,_->finish()}.show()}
}
