package ru.vpnc.quiclab

import android.app.Activity
import android.graphics.Color
import android.graphics.Typeface
import android.graphics.drawable.GradientDrawable
import android.view.View
import android.view.WindowInsets
import android.widget.*

internal class ClientUi(val activity:Activity) {
 val ink=Color.rgb(23,44,60);val muted=Color.rgb(102,121,134);val accent=Color.rgb(0,128,117);val surface=Color.rgb(244,247,249)
 fun dp(n:Int)=(n*activity.resources.displayMetrics.density).toInt()
 fun background(color:Int)=GradientDrawable().apply{setColor(color);cornerRadius=dp(14).toFloat()}
 fun column()=LinearLayout(activity).apply{orientation=LinearLayout.VERTICAL}
 fun text(value:String,size:Float=14f,bold:Boolean=false)=TextView(activity).apply{text=value;textSize=size;setTextColor(if(bold)ink else muted);if(bold)setTypeface(null,Typeface.BOLD);setPadding(0,dp(5),0,dp(5))}
 fun button(value:String,primary:Boolean=false,action:()->Unit)=Button(activity).apply{text=value;isAllCaps=false;stateListAnimator=null;elevation=0f;textSize=14f;setTextColor(if(primary)Color.WHITE else accent);background=background(if(primary)accent else Color.rgb(235,245,243));minHeight=dp(48);setPadding(dp(12),dp(8),dp(12),dp(8));setOnClickListener{action()}}
 fun card(parent:LinearLayout):LinearLayout=column().apply{background=background(Color.WHITE);setPadding(dp(16),dp(12),dp(16),dp(12));parent.addView(this,LinearLayout.LayoutParams(-1,-2).apply{bottomMargin=dp(12)})}
 fun add(parent:LinearLayout,view:View){parent.addView(view,LinearLayout.LayoutParams(-1,-2).apply{topMargin=dp(8)})}
 fun install(root:LinearLayout){root.setBackgroundColor(surface);activity.window.statusBarColor=surface;activity.window.navigationBarColor=surface;activity.window.decorView.systemUiVisibility=View.SYSTEM_UI_FLAG_LIGHT_STATUS_BAR or View.SYSTEM_UI_FLAG_LIGHT_NAVIGATION_BAR;root.setOnApplyWindowInsetsListener{_,i->val b=i.getInsets(WindowInsets.Type.systemBars() or WindowInsets.Type.displayCutout());root.setPadding(b.left,b.top,b.right,b.bottom);i};activity.setContentView(root)}
}
