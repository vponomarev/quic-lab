package ru.vpnc.quiclab

import android.app.Activity
import android.content.Intent
import android.content.pm.ApplicationInfo
import android.graphics.Color
import android.os.Bundle
import android.text.Editable
import android.text.TextWatcher
import android.view.View
import android.view.ViewGroup
import android.view.WindowInsets
import android.widget.*
import java.util.concurrent.Executors

class AppSelectionActivity : Activity() {
    private data class App(val id: String, val name: String, val system: Boolean)

    private val worker = Executors.newSingleThreadExecutor()
    private var all = emptyList<App>()
    private var visible = emptyList<App>()
    private val selected = mutableSetOf<String>()
    private lateinit var search: EditText
    private lateinit var system: CheckBox
    private lateinit var status: TextView
    private lateinit var adapter: BaseAdapter
    private var loaded = false

    private fun dp(n: Int) = (n * resources.displayMetrics.density).toInt()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        selected.addAll(
            savedInstanceState?.getStringArrayList("selected")
                ?: intent.getStringArrayListExtra("apps")
                ?: emptyList()
        )
        val panel =
            LinearLayout(this).apply {
                orientation = LinearLayout.VERTICAL
                setBackgroundColor(Color.rgb(240, 245, 248))
                setPadding(dp(16), dp(12), dp(16), dp(12))
            }
        setContentView(panel)
        panel.setOnApplyWindowInsetsListener { _, ins ->
            val bars = ins.getInsets(WindowInsets.Type.systemBars())
            panel.setPadding(
                dp(16) + bars.left,
                dp(12) + bars.top,
                dp(16) + bars.right,
                dp(12) + bars.bottom,
            )
            ins
        }
        panel.addView(
            TextView(this).apply {
                text = intent.getStringExtra("title") ?: "Приложения для VPN"
                textSize = 25f
            }
        )
        panel.addView(
            TextView(this).apply {
                text = "Выбор применяется в режимах Only selected apps и Exclude selected apps."
                setPadding(0, dp(8), 0, dp(8))
            }
        )
        search =
            EditText(this).apply {
                hint = "Поиск по названию или ID"
                setSingleLine(true)
                setText(savedInstanceState?.getString("query", "") ?: "")
            }
        panel.addView(search)
        system =
            CheckBox(this).apply {
                text = "Показать системные приложения"
                isChecked = savedInstanceState?.getBoolean("system") ?: false
            }
        panel.addView(system)
        status =
            TextView(this).apply {
                text = "Загружаем приложения…"
                setPadding(0, dp(8), 0, dp(8))
            }
        panel.addView(status)
        adapter =
            object : BaseAdapter() {
                override fun getCount() = visible.size

                override fun getItem(position: Int) = visible[position]

                override fun getItemId(position: Int) = position.toLong()

                override fun getView(position: Int, convertView: View?, parent: ViewGroup): View {
                    val row =
                        (convertView as? LinearLayout)
                            ?: LinearLayout(this@AppSelectionActivity).apply {
                                orientation = LinearLayout.HORIZONTAL
                                gravity = android.view.Gravity.CENTER_VERTICAL
                                setPadding(0, dp(8), 0, dp(8))
                                addView(
                                    CheckBox(context).apply {
                                        isClickable = false
                                        isFocusable = false
                                        importantForAccessibility =
                                            View.IMPORTANT_FOR_ACCESSIBILITY_NO
                                    }
                                )
                                addView(
                                    LinearLayout(context).apply {
                                        orientation = LinearLayout.VERTICAL
                                        addView(
                                            TextView(context).apply {
                                                textSize = 17f
                                                setTextColor(Color.rgb(20, 40, 58))
                                            }
                                        )
                                        addView(
                                            TextView(context).apply {
                                                textSize = 12f
                                                setTextColor(Color.rgb(94, 112, 127))
                                            }
                                        )
                                    },
                                    LinearLayout.LayoutParams(
                                        0,
                                        ViewGroup.LayoutParams.WRAP_CONTENT,
                                        1f,
                                    ),
                                )
                            }
                    val app = visible[position]
                    (row.getChildAt(0) as CheckBox).isChecked = app.id in selected
                    val texts = row.getChildAt(1) as LinearLayout
                    (texts.getChildAt(0) as TextView).text = app.name
                    (texts.getChildAt(1) as TextView).text = app.id
                    row.contentDescription =
                        "${app.name}, ${if(app.id in selected) "выбрано" else "не выбрано"}"
                    return row
                }
            }
        panel.addView(
            ListView(this).apply {
                adapter = this@AppSelectionActivity.adapter
                setOnItemClickListener { _, _, position, _ ->
                    val id = visible[position].id
                    if (!selected.add(id)) selected.remove(id)
                    refresh()
                }
            },
            LinearLayout.LayoutParams(ViewGroup.LayoutParams.MATCH_PARENT, 0, 1f),
        )
        val actions = LinearLayout(this)
        actions.addView(
            Button(this).apply {
                text = "Отмена"
                isAllCaps = false
                setOnClickListener { finish() }
            },
            LinearLayout.LayoutParams(0, dp(56), 1f),
        )
        actions.addView(
            Button(this).apply {
                text = "Сохранить"
                isAllCaps = false
                setOnClickListener {
                    setResult(
                        RESULT_OK,
                        Intent().putStringArrayListExtra("apps", ArrayList(selected)),
                    )
                    finish()
                }
            },
            LinearLayout.LayoutParams(0, dp(56), 1f),
        )
        panel.addView(actions)
        search.addTextChangedListener(
            object : TextWatcher {
                override fun beforeTextChanged(
                    s: CharSequence?,
                    start: Int,
                    count: Int,
                    after: Int,
                ) {}

                override fun onTextChanged(s: CharSequence?, start: Int, before: Int, count: Int) {
                    refresh()
                }

                override fun afterTextChanged(s: Editable?) {}
            }
        )
        system.setOnCheckedChangeListener { _, _ -> refresh() }
        worker.execute {
            try {
                val launcher = Intent(Intent.ACTION_MAIN).addCategory(Intent.CATEGORY_LAUNCHER)
                val launchable =
                    packageManager.queryIntentActivities(launcher, 0).associate { info ->
                        info.activityInfo.packageName to info.loadLabel(packageManager).toString()
                    }
                val items =
                    packageManager
                        .getInstalledApplications(0)
                        .filter { it.packageName != packageName }
                        .map { app ->
                            val label =
                                launchable[app.packageName]
                                    ?: app.loadLabel(packageManager).toString()
                            App(
                                app.packageName,
                                label,
                                (app.flags and ApplicationInfo.FLAG_SYSTEM) != 0 &&
                                    app.packageName !in launchable,
                            )
                        }
                        .sortedWith(
                            compareBy<App, String>(String.CASE_INSENSITIVE_ORDER) { it.name }
                                .thenBy { it.id }
                        )
                runOnUiThread {
                    if (!isDestroyed && !isFinishing) {
                        all = items
                        loaded = true
                        refresh()
                    }
                }
            } catch (e: Exception) {
                runOnUiThread {
                    if (!isDestroyed) status.text = "Не удалось загрузить список приложений"
                }
            }
        }
    }

    private fun refresh() {
        if (!loaded) return
        val query = search.text.toString().trim()
        visible =
            all.filter {
                (system.isChecked || !it.system || it.id in selected) &&
                    (it.name.contains(query, true) || it.id.contains(query, true))
            }
        visible = visible.sortedBy { if(it.id in selected) 0 else 1 }
        status.text =
            "Выбрано: ${selected.size} · Найдено: ${visible.size}" +
                if (visible.isEmpty()) " — ничего не найдено" else ""
        adapter.notifyDataSetChanged()
    }

    override fun onSaveInstanceState(out: Bundle) {
        out.putStringArrayList("selected", ArrayList(selected))
        out.putString("query", search.text.toString())
        out.putBoolean("system", system.isChecked)
        super.onSaveInstanceState(out)
    }

    override fun onDestroy() {
        worker.shutdownNow()
        super.onDestroy()
    }
}
