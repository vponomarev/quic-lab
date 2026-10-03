package ru.vpnc.quiclab

import android.graphics.Color
import android.graphics.Typeface
import android.text.SpannableString
import android.text.Spanned
import android.text.style.ForegroundColorSpan
import android.text.style.StyleSpan

/** Compact foreground radio diagnostics, with explicit cell identity rows. */
internal object RadioSummary {
    fun text(wifi: String, cell: String): SpannableString {
        val wifiLine = wifi.removePrefix("Wi-Fi:").removePrefix("Wi-Fi SSID/BSSID:")
            .replace("\nBSSID (MAC точки):", " ·")
            .replace("\nСигнал", " ·").substringBefore(" · линк")
            .replace(Regex(" · \\d+ МГц"), "").trim()
        val lines = cell.removePrefix("Сотовая сеть (SIM данных):")
            .removePrefix("Сотовая сеть:").removePrefix("Сота:").trim().lines()
        val cellRows = if (lines.size >= 2 && lines[1].startsWith("Сигнал ")) {
            lines.chunked(2).map { pair ->
                val parts = pair[0].split(" · ")
                val signal = pair.getOrNull(1).orEmpty().removePrefix("Сигнал ")
                val dbm = signal.substringBefore(" · ").replace(" dBm", "dBm")
                val age = Regex("данные (\\d+) с назад").find(signal)?.groupValues?.get(1)
                val ageLabel = age?.let {
                    val n=it.toLong(); val word=when { n%100 in 11..14 -> "секунд"; n%10==1L -> "секунду"; n%10 in 2..4 -> "секунды"; else -> "секунд" }
                    " ($it $word назад)"
                }.orEmpty()
                "GSM: ${parts.take(2).joinToString(" ")} $dbm$ageLabel\n${parts.drop(2).joinToString(" · ")}"
            }.joinToString("\n")
        } else "GSM: ${lines.joinToString(" · ")}"
        return SpannableString("WiFi: $wifiLine\n$cellRows").apply {
            val gsmStart = indexOf("\n") + 1
            setSpan(ForegroundColorSpan(Color.rgb(72, 91, 145)), gsmStart, length, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            Regex("(?:-?\\d+|—) ?dBm").findAll(this).forEach {
                setSpan(StyleSpan(Typeface.BOLD), it.range.first, it.range.last + 1, Spanned.SPAN_EXCLUSIVE_EXCLUSIVE)
            }
        }
    }
}
