package ru.vpnc.quiclab

import android.text.style.StyleSpan
import org.junit.Assert.*
import org.junit.Test

class RadioSummaryTest {
 @Test fun bothRadiosKeepIdentitiesAndBoldSignals() {
  val text=RadioSummary.text("Wi-Fi: test\nBSSID (MAC точки): aa:bb:cc:dd:ee:ff\nСигнал -51 dBm · 5180 МГц · линк 433 Мбит/с", "Сотовая сеть (SIM данных):\nLTE · 250/01 · CI 123 · TAC 45 · PCI 6 · EARFCN 100\nСигнал -91 dBm · данные 2 с назад")
  assertEquals(3,text.lines().size)
  assertTrue(text.toString().startsWith("WiFi: test · aa:bb:cc:dd:ee:ff · -51 dBm"))
  assertTrue(text.toString().contains("GSM: LTE 250/01 -91dBm (2 секунды назад)\nCI 123 · TAC 45 · PCI 6 · EARFCN 100"))
  assertEquals(1,text.getSpans(0,text.length,android.text.style.ForegroundColorSpan::class.java).size)
  val spans=text.getSpans(0,text.length,StyleSpan::class.java)
  assertEquals(setOf("-51 dBm","-91dBm"),spans.map{text.subSequence(text.getSpanStart(it),text.getSpanEnd(it)).toString()}.toSet())
 }
}
