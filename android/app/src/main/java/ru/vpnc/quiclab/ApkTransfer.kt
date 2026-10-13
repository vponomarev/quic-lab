package ru.vpnc.quiclab

import java.io.*
import java.net.URL
import javax.net.ssl.HttpsURLConnection

/** Runs as the application's UID: never delegates network selection to DownloadManager. */
internal object ApkTransfer {
 const val MAX_BYTES=268435456L
 fun redirect(base:String,location:String):String {
  val url=URL(URL(base),location)
  require(url.protocol=="https" && url.userInfo==null){"Обновление требует безопасного HTTPS-адреса"}
  return url.toString()
 }
 fun copyBody(input:InputStream,output:OutputStream,total:Long,max:Long,active:()->Boolean,progress:(Long)->Unit){
  require(total<=max){"APK превышает допустимый размер"}
  val buffer=ByteArray(65536);var count=0L
  while(true){
   if(!active())throw InterruptedIOException("Скачивание отменено")
   val n=input.read(buffer);if(n<0)break
   count+=n;require(count<=max){"APK превышает допустимый размер"}
   output.write(buffer,0,n);progress(count)
  }
  require(count>0 && (total<0 || count==total)){"Загрузка оборвалась. Повторите скачивание"}
 }
 fun download(url:String,file:File,active:()->Boolean,connection:(HttpsURLConnection?)->Unit,progress:(Long,Long)->Unit){
  var address=redirect(url,url)
  try {
   for(hop in 0..5){
    if(!active())throw InterruptedIOException("Скачивание отменено")
    val conn=URL(address).openConnection() as HttpsURLConnection
    conn.connectTimeout=15000;conn.readTimeout=15000;conn.instanceFollowRedirects=false
    conn.setRequestProperty("Accept-Encoding","identity")
    connection(conn)
    try {
     val code=conn.responseCode
     if(code in listOf(301,302,303,307,308)){
      check(hop<5){"Слишком много перенаправлений"}
      address=redirect(address,requireNotNull(conn.getHeaderField("Location")){"Нет адреса перенаправления"});continue
     }
     check(code==200){"Сервер ответил HTTP $code"}
     val total=conn.contentLengthLong;progress(0,total)
     conn.inputStream.use{input->file.outputStream().use{output->copyBody(input,output,total,MAX_BYTES,active){progress(it,total)}}}
     return
    }finally{connection(null);conn.disconnect()}
   }
  }catch(e:Exception){file.delete();throw e}
 }
}
