package ru.vpnc.quiclab

import android.content.SharedPreferences

/** An editor's private snapshot. Nothing reaches the runtime until persist(). */
internal class PreferenceDraft(private val source: SharedPreferences) : SharedPreferences by source {
    private fun copy(values: Map<String, *>): MutableMap<String, Any?> = values.mapValues { (_,v) -> if(v is Set<*>) v.toSet() else v }.toMutableMap()
    private var original = copy(source.all)
    private var values = copy(original)
    private var baseline = copy(values)
    val dirty: Boolean get() = values != baseline
    fun acceptInitialState() { baseline = copy(values) }
    fun snapshot(): java.util.HashMap<String, Any?> = java.util.HashMap(copy(values))
    fun restore(saved: Map<String, *>) { values = copy(saved) }
    fun persist() {
        val editor = source.edit()
        for (key in original.keys + values.keys) {
            if (original[key] == values[key] && original.containsKey(key) == values.containsKey(key)) continue
            when (val value = values[key]) {
                null -> editor.remove(key)
                is String -> editor.putString(key,value)
                is Boolean -> editor.putBoolean(key,value)
                is Int -> editor.putInt(key,value)
                is Long -> editor.putLong(key,value)
                is Float -> editor.putFloat(key,value)
                is Set<*> -> editor.putStringSet(key,value.filterIsInstance<String>().toSet())
                else -> error("Unsupported preference type")
            }
        }
        check(editor.commit()) { "Не удалось сохранить настройки" }
        original = copy(values); baseline = copy(values)
    }
    override fun getAll(): MutableMap<String, *> = copy(values)
    override fun contains(key:String) = values.containsKey(key)
    override fun getString(key:String,defValue:String?):String? = values[key] as? String ?: defValue
    override fun getBoolean(key:String,defValue:Boolean) = values[key] as? Boolean ?: defValue
    override fun getInt(key:String,defValue:Int) = values[key] as? Int ?: defValue
    override fun getLong(key:String,defValue:Long) = values[key] as? Long ?: defValue
    override fun getFloat(key:String,defValue:Float) = values[key] as? Float ?: defValue
    override fun getStringSet(key:String,defValues:Set<String>?):MutableSet<String>? = (values[key] as? Set<*>)?.filterIsInstance<String>()?.toMutableSet() ?: defValues?.toMutableSet()
    override fun edit():SharedPreferences.Editor = object:SharedPreferences.Editor {
        private val edits=mutableMapOf<String,Any?>(); private var clear=false
        override fun putString(k:String?,v:String?)=apply{if(k!=null)edits[k]=v}
        override fun putStringSet(k:String?,v:MutableSet<String>?)=apply{if(k!=null)edits[k]=v?.toSet()}
        override fun putInt(k:String?,v:Int)=apply{if(k!=null)edits[k]=v}
        override fun putLong(k:String?,v:Long)=apply{if(k!=null)edits[k]=v}
        override fun putFloat(k:String?,v:Float)=apply{if(k!=null)edits[k]=v}
        override fun putBoolean(k:String?,v:Boolean)=apply{if(k!=null)edits[k]=v}
        override fun remove(k:String?)=apply{if(k!=null)edits[k]=null}
        override fun clear()=apply{clear=true}
        override fun commit():Boolean {if(clear)values.clear();edits.forEach{(k,v)->if(v==null)values.remove(k)else values[k]=v};return true}
        override fun apply(){commit()}
    }
}
