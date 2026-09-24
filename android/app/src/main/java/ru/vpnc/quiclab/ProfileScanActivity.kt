package ru.vpnc.quiclab
import android.app.Activity
import android.app.AlertDialog
import android.content.Intent
import android.os.Bundle
import android.widget.TextView
import com.google.zxing.integration.android.IntentIntegrator
import org.json.JSONObject

class ProfileScanActivity : Activity() {
    override fun onCreate(savedInstanceState:Bundle?) {
        super.onCreate(savedInstanceState)
        setContentView(TextView(this).apply{text="Импорт профиля QUIC Lab…";setPadding(24,72,24,24)})
        if(savedInstanceState==null) IntentIntegrator(this).setDesiredBarcodeFormats(IntentIntegrator.QR_CODE)
            .setPrompt("QR echo или VPN из QUIC Lab").setBeepEnabled(false).setOrientationLocked(false).initiateScan()
    }
    @Deprecated("Activity result compatibility")
    override fun onActivityResult(requestCode:Int,resultCode:Int,data:Intent?) {
        val scan=IntentIntegrator.parseActivityResult(requestCode,resultCode,data)
        if(scan==null){super.onActivityResult(requestCode,resultCode,data);return}
        val raw=scan.contents ?: run{finish();return}
        try {
            require(raw.length<=8192){"QR слишком большой"}
            if(raw.trimStart().startsWith("{")) review(JSONObject(raw))
            else {val uri=ProfileImport.enrollment(raw)
                AlertDialog.Builder(this).setTitle("Получить VPN-профиль?")
                    .setMessage("Сервер: ${uri.host}\nБудут получены настройки, клиентский сертификат и закрытый ключ.")
                    .setNegativeButton("Отмена"){_,_->finish()}.setPositiveButton("Получить"){_,_->
                        Thread{try{val p=ProfileImport.fetch(raw);runOnUiThread{if(!isFinishing)review(p)}}catch(e:Exception){runOnUiThread{fail(e)}}}.start()
                    }.show()
            }
        }catch(e:Exception){fail(e)}
    }
    private fun review(p:JSONObject){val kind=ProfileImport.validate(p)
        val address=if(kind=="echo")p.getString("endpoint") else "QUIC: ${p.getString("quic")}\nHTTPS: ${p.getString("https")}"
        AlertDialog.Builder(this).setTitle(if(kind=="echo")"Настройки echo" else "VPN: ${p.optString("name")}")
            .setMessage("$address\nTLS: ${p.getString("hostname")}\n${if(kind=="vpn") "Добавить новый VPN-профиль?" else "Заменить настройки echo?"}")
            .setNegativeButton("Отмена"){_,_->finish()}.setPositiveButton("Сохранить"){_,_->try{
                val saved=ProfileImport.save(this,p);setResult(RESULT_OK,Intent().putExtra("kind",saved));finish()
            }catch(e:Exception){fail(e)}}.show()
    }
    private fun fail(e:Exception){if(isFinishing)return;AlertDialog.Builder(this).setTitle("Импорт не выполнен").setMessage(e.message ?: "Ошибка профиля").setPositiveButton("OK"){_,_->finish()}.show()}
}
