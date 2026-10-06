package ru.vpnc.quiclab

import android.content.ContentValues
import android.content.Context
import android.database.sqlite.SQLiteDatabase
import android.database.sqlite.SQLiteOpenHelper
import java.util.UUID
import org.json.JSONObject

internal object DiagnosticsPolicy {
 const val DEFAULT_ENABLED=true
 fun autoAllowed(enabled:Boolean,wifi:Boolean,unmetered:Boolean,validated:Boolean)=enabled&&wifi&&unmetered&&validated
 fun preferences(c:Context)=c.getSharedPreferences("diagnostics_settings",Context.MODE_PRIVATE)
 fun enabled(c:Context)=preferences(c).getBoolean("enabled",DEFAULT_ENABLED)
 fun detailed(c:Context)=preferences(c).getBoolean("detailed",false)
}
internal class DiagnosticsJournal(c:Context,name:String="diagnostics-v2.db"):SQLiteOpenHelper(c,name,null,1) {
 data class Record(val id:String,val time:Long,val kind:String,val text:String){
  fun json()=JSONObject().put("id",id).put("time",time).put("kind",kind).put("text",text)
 }
 override fun onCreate(db:SQLiteDatabase){
  db.execSQL("CREATE TABLE records(seq INTEGER PRIMARY KEY AUTOINCREMENT,id TEXT UNIQUE NOT NULL,profile TEXT NOT NULL,target TEXT NOT NULL,time INTEGER NOT NULL,kind TEXT NOT NULL,text TEXT NOT NULL,size INTEGER NOT NULL,acked INTEGER NOT NULL DEFAULT 0)")
  db.execSQL("CREATE INDEX pending ON records(profile,target,acked,seq)")
 }
 override fun onUpgrade(db:SQLiteDatabase,old:Int,new:Int){}
 @Synchronized fun append(profile:String,target:String,kind:String,text:String,time:Long=System.currentTimeMillis()){
  require(kind in listOf("event","summary","detail"))
  var bounded=text.take(1800);while(bounded.toByteArray(Charsets.UTF_8).size>2000)bounded=bounded.dropLast(1)
  val v=ContentValues().apply {put("id",UUID.randomUUID().toString());put("profile",profile);put("target",target);put("time",time);put("kind",kind);put("text",bounded);put("size",bounded.toByteArray().size+256)}
  writableDatabase.insertOrThrow("records",null,v)
 }
 private fun query(where:String,args:Array<String>,limit:Int):List<Record>{
  val result=mutableListOf<Record>()
  readableDatabase.rawQuery("SELECT id,time,kind,text FROM records $where LIMIT $limit",args).use{while(it.moveToNext()) result.add(Record(it.getString(0),it.getLong(1),it.getString(2),it.getString(3)))}
  return result
 }
 @Synchronized fun pending(profile:String,target:String)=query("WHERE profile=? AND target=? AND acked=0 ORDER BY seq",arrayOf(profile,target),128)
 @Synchronized fun recent()=query("ORDER BY seq DESC",emptyArray(),400)
 @Synchronized fun targets():List<Pair<String,String>>{
  val result=mutableListOf<Pair<String,String>>()
  readableDatabase.rawQuery("SELECT DISTINCT profile,target FROM records WHERE acked=0",null).use{while(it.moveToNext())result.add(it.getString(0) to it.getString(1))}
  return result
 }
 @Synchronized fun ack(ids:List<String>){
  val db=writableDatabase;db.beginTransaction()
  try{val v=ContentValues().apply{put("acked",1)};ids.forEach{db.update("records",v,"id=?",arrayOf(it))};db.setTransactionSuccessful()}finally{db.endTransaction()}
 }
 @Synchronized fun bytes(pending:Boolean=false):Long=readableDatabase.rawQuery("SELECT COALESCE(SUM(size),0) FROM records"+if(pending)" WHERE acked=0" else "",null).use{it.moveToFirst();it.getLong(0)}
 @Synchronized fun clear(){writableDatabase.delete("records",null,null)}
 @Synchronized fun prune(now:Long=System.currentTimeMillis()){
  val db=writableDatabase;db.beginTransaction()
  try{
   for((kind,days,mib)in listOf(Triple("event",7,5),Triple("summary",3,10),Triple("detail",1,20))){
    db.delete("records","kind=? AND time<?",arrayOf(kind,(now-days*86400000L).toString()))
    var bytes=db.rawQuery("SELECT COALESCE(SUM(size),0) FROM records WHERE kind=?",arrayOf(kind)).use{it.moveToFirst();it.getLong(0)}
    if(bytes>mib*1048576L){
     val remove=mutableListOf<String>()
     db.rawQuery("SELECT id,size FROM records WHERE kind=? ORDER BY seq",arrayOf(kind)).use{while(bytes>mib*1048576L&&it.moveToNext()){remove.add(it.getString(0));bytes-=it.getLong(1)}}
     remove.forEach{db.delete("records","id=?",arrayOf(it))}
    }
   }
   db.setTransactionSuccessful()
  }finally{db.endTransaction()}
 }
}
