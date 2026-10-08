package ru.vpnc.quiclab

import android.content.Context
import android.content.SharedPreferences

/** A source retains the management epoch from when an editor opened it. */
internal interface ConfigurationEditGuard { fun validateWrite(); fun editGeneration():String }

internal class ConfigurationPreferences(private val c:Context,private val name:String):SharedPreferences,ConfigurationEditGuard {
 private val epoch=MdmConfiguration.token(c)
 override fun editGeneration()="${epoch.first}:${epoch.second}"
 override fun validateWrite()=synchronized(MdmConfiguration.lock){MdmConfiguration.assertEditable(c,epoch)}
 private fun values()=MdmConfiguration.values(c,name)
 override fun getAll():MutableMap<String,*> =values().toMutableMap()
 override fun contains(key:String)=values().containsKey(key)
 override fun getString(key:String,defValue:String?):String?=values()[key] as? String ?: defValue
 override fun getBoolean(key:String,defValue:Boolean)=values()[key] as? Boolean ?: defValue
 override fun getInt(key:String,defValue:Int)=(values()[key] as? Number)?.toInt() ?: defValue
 override fun getLong(key:String,defValue:Long)=(values()[key] as? Number)?.toLong() ?: defValue
 override fun getFloat(key:String,defValue:Float)=(values()[key] as? Number)?.toFloat() ?: defValue
 override fun getStringSet(key:String,defValues:Set<String>?):MutableSet<String>?=(values()[key] as? Set<*>)?.filterIsInstance<String>()?.toMutableSet() ?: defValues?.toMutableSet()
 override fun registerOnSharedPreferenceChangeListener(listener:SharedPreferences.OnSharedPreferenceChangeListener){synchronized(listeners){listeners.getOrPut(key(c,name)){java.util.WeakHashMap()}[listener]=true}}
 override fun unregisterOnSharedPreferenceChangeListener(listener:SharedPreferences.OnSharedPreferenceChangeListener){synchronized(listeners){listeners[key(c,name)]?.remove(listener)}}
 override fun edit():SharedPreferences.Editor=object:SharedPreferences.Editor{
  private var clear=false
  private val edits=mutableMapOf<String,Any?>()
  override fun putString(k:String?,v:String?)=apply{if(k!=null)edits[k]=v}
  override fun putStringSet(k:String?,v:MutableSet<String>?)=apply{if(k!=null)edits[k]=v?.toSet()}
  override fun putBoolean(k:String?,v:Boolean)=apply{if(k!=null)edits[k]=v}
  override fun putInt(k:String?,v:Int)=apply{if(k!=null)edits[k]=v}
  override fun putLong(k:String?,v:Long)=apply{if(k!=null)edits[k]=v}
  override fun putFloat(k:String?,v:Float)=apply{if(k!=null)edits[k]=v}
  override fun remove(k:String?)=apply{if(k!=null)edits[k]=null}
  override fun clear()=apply{clear=true}
  override fun commit()=MdmConfiguration.edit(c,name,epoch,clear,edits)
  override fun apply(){check(commit()){"Не удалось сохранить настройки"}}
 }

 companion object {
  private val listeners=mutableMapOf<String,java.util.WeakHashMap<SharedPreferences.OnSharedPreferenceChangeListener,Boolean>>()
  private fun key(c:Context,name:String)=c.filesDir.absolutePath+"|"+name
  fun notify(c:Context,name:String,keys:Set<String>){
   val snapshot=synchronized(listeners){listeners[key(c,name)]?.keys?.toList().orEmpty()}
   if(snapshot.isEmpty())return
   android.os.Handler(android.os.Looper.getMainLooper()).post{
    val prefs=ConfigurationPreferences(c,name)
    snapshot.forEach{listener->keys.forEach{listener.onSharedPreferenceChanged(prefs,it)}}
   }
  }
 }
}
