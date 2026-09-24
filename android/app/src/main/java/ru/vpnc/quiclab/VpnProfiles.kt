package ru.vpnc.quiclab

import android.content.Context
import android.content.SharedPreferences
import java.io.File
import java.util.UUID
import org.json.JSONArray
import org.json.JSONObject

internal object VpnProfiles {
    data class Profile(val id:String,val name:String)
    private fun meta(c:Context)=c.getSharedPreferences("vpn_profiles",Context.MODE_PRIVATE)
    @Synchronized fun list(c:Context):List<Profile> {
        val m=meta(c)
        if(!m.contains("profiles")) {
            val old=c.getSharedPreferences("vpn",Context.MODE_PRIVATE)
            check(m.edit().putString("profiles",JSONArray().put(JSONObject().put("id","default").put("name","Основной")).toString())
                .putString("current","default").putStringSet("global_apps",old.getStringSet("apps",emptySet())).commit())
        }
        val a=JSONArray(m.getString("profiles","[]"))
        return (0 until a.length()).map { val p=a.getJSONObject(it);Profile(p.getString("id"),p.getString("name")) }
    }
    fun current(c:Context):Profile {
        val profiles=list(c)
        return profiles.firstOrNull{it.id==meta(c).getString("current",null)} ?: profiles.first()
    }
    fun preferences(c:Context,id:String=current(c).id):SharedPreferences {
        require(id=="default" || id.matches(Regex("[a-f0-9-]{36}")))
        return c.getSharedPreferences(if(id=="default") "vpn" else "vpn_$id",Context.MODE_PRIVATE)
    }
    fun identityFile(c:Context,id:String=current(c).id)=File(c.filesDir,if(id=="default") "vpn-identity.enc" else "vpn-identity-$id.enc")
    private fun write(c:Context,profiles:List<Profile>,current:String) {
        val a=JSONArray();profiles.forEach{a.put(JSONObject().put("id",it.id).put("name",it.name))}
        check(meta(c).edit().putString("profiles",a.toString()).putString("current",current).commit())
    }
    private fun editable(){check(!LabVpnService.active){"Сначала остановите VPN"}}
    private fun name(value:String)=value.trim().also{require(it.isNotBlank() && it.length<=100){"Название: от 1 до 100 символов"}}
    @Synchronized fun create(c:Context,title:String):Profile {
        editable();val profiles=list(c);require(profiles.size<32){"Не более 32 профилей"}
        val p=Profile(UUID.randomUUID().toString(),name(title))
        check(preferences(c,p.id).edit().putBoolean("global_apps",true).commit())
        write(c,profiles+p,p.id);return p
    }
    @Synchronized fun select(c:Context,id:String){editable();require(list(c).any{it.id==id});check(meta(c).edit().putString("current",id).commit())}
    @Synchronized fun rename(c:Context,title:String){editable();val id=current(c).id;val n=name(title);write(c,list(c).map{if(it.id==id)it.copy(name=n)else it},id)}
    @Synchronized fun delete(c:Context,id:String=current(c).id){
        editable();val profiles=list(c);require(profiles.size>1){"Нельзя удалить последний профиль"}
        val rest=profiles.filter{it.id!=id};require(rest.size<profiles.size)
        val selected=current(c).id
        write(c,rest,if(selected==id)rest.first().id else selected)
        preferences(c,id).edit().clear().commit();identityFile(c,id).delete()
    }
    fun globalApps(c:Context)=meta(c).getStringSet("global_apps",emptySet())!!.toSet()
    fun setGlobalApps(c:Context,apps:Set<String>){check(meta(c).edit().putStringSet("global_apps",apps.toSet()).commit())}
    fun apps(c:Context):Set<String> {
        val p=preferences(c)
        return if(p.getBoolean("global_apps",false)) globalApps(c) else p.getStringSet("apps",emptySet())!!.toSet()
    }
}
