package ru.vpnc.quiclab

import android.content.Context
import android.graphics.Canvas
import android.graphics.Color
import android.graphics.Paint
import android.graphics.Path
import android.view.View
import android.os.SystemClock

internal class LatencyChart(context: Context) : View(context) {
    private data class Sample(val time: Long, val first: Float?, val second: Float?, val third: Float?)
    private val points = ArrayDeque<Sample>()
    private val markers = ArrayDeque<Long>()
    private var end = 0L
    private val paint = Paint(Paint.ANTI_ALIAS_FLAG)
    private val teal = Color.rgb(0, 128, 117)
    private val amber = Color.rgb(186, 105, 17)
    fun sample(quic: Float?, wss: Float?, awg: Float? = null) {
        end = SystemClock.elapsedRealtime()
        points.addLast(Sample(end, quic, wss, awg))
        while (points.isNotEmpty() && points.first().time < end - 30000) points.removeFirst()
        while (markers.isNotEmpty() && markers.first() < end - 30000) markers.removeFirst()
        invalidate()
    }
    fun mark(time: Long) {
        if (markers.lastOrNull()?.let { time - it < 150 } == true) return
        markers.addLast(time)
        while (markers.size > 120) markers.removeFirst()
        invalidate()
    }
    fun clear() { points.clear(); markers.clear(); end = 0; invalidate() }
    override fun onDraw(canvas: Canvas) {
        super.onDraw(canvas)
        val density = resources.displayMetrics.density
        val top = 22 * density
        val bottom = height - 20 * density
        val scale = maxOf(100f, points.flatMap { listOfNotNull(it.first, it.second, it.third) }.maxOrNull() ?: 100f)
        paint.style = Paint.Style.FILL
        paint.textSize = 11 * density
        paint.color = Color.rgb(95, 112, 128)
        canvas.drawText("${scale.toInt()} мс", 0f, 13 * density, paint)
        canvas.drawText("30 секунд · RTT", 0f, height - 2 * density, paint)
        paint.color = Color.rgb(224, 231, 237)
        paint.strokeWidth = density
        for (i in 0..2) {
            val y = top + (bottom - top) * i / 2
            canvas.drawLine(0f, y, width.toFloat(), y, paint)
        }
        paint.color = Color.rgb(123, 85, 155)
        markers.filter { it in (end - 30000)..end }.forEach { time ->
            val x = width * (1f - (end - time) / 30000f)
            canvas.drawLine(x, top, x, bottom, paint)
        }
        for (series in 0..2) {
            val path = Path()
            var started = false
            points.forEach { pair ->
                val value = when (series) { 0 -> pair.first; 1 -> pair.second; else -> pair.third }
                if (value == null) started = false else {
                    val x = width * (1f - (end - pair.time) / 30000f)
                    val y = bottom - (bottom - top) * value.coerceAtLeast(0f) / scale
                    if (started) path.lineTo(x, y) else { path.moveTo(x, y); started = true }
                }
            }
            paint.color = when (series) { 0 -> teal; 1 -> amber; else -> Color.rgb(100, 80, 175) }
            paint.style = Paint.Style.STROKE
            paint.strokeWidth = 2 * density
            canvas.drawPath(path, paint)
        }
        paint.style = Paint.Style.FILL
    }
}
