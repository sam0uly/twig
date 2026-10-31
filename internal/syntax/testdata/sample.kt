package sample

/** A documented class. */
data class Point(val x: Int, val y: Int) {
    // a comment with a "quote"
    fun length(): Double {
        val total = 3.5 + 0x1F
        return Math.sqrt((x * x + y * y).toDouble()) + total
    }
}

fun main() {
    val p = Point(1, 2)
    println("length ${p.length()}")
}
