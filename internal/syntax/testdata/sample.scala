package fun.twig

import scala.collection.mutable

/** A bounded cache of recent keys. */
final class Cache(capacity: Int = 16):
  private val slots = mutable.ArrayDeque.empty[String]

  def push(key: String): Unit =
    if slots.size >= capacity then slots.removeHead()
    slots.append(key)

  def contains(key: String): Boolean = slots.contains(key)

  def report: String =
    s"""{"kind": "cache", "size": ${slots.size}}"""

object Cache:
  def empty: Cache = Cache()

  // Hex, binary and a character literal.
  val mask = 0xff & 0b1111
  val delim = ';'
  val ratio = 1.5e3

  /* Scala block comments
     nest like this /* inner */ one. */
