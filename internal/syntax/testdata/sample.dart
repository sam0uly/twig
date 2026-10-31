import 'dart:collection';

/// A bounded cache of recent keys.
class Cache {
  final int capacity;
  final Queue<String> _slots = Queue<String>();

  Cache({this.capacity = 16});

  void push(String key) {
    if (_slots.length >= capacity) _slots.removeFirst();
    _slots.add(key);
  }

  bool contains(String key) => _slots.contains(key);

  String report() {
    return '''
    {"kind": "cache", "size": ${_slots.length}}
    ''';
  }
}

void main() {
  final cache = Cache(capacity: 0x10);
  for (var i = 0; i < 4; i++) {
    cache.push('key-$i');
  }
  print(cache.report());

  /* Block comments nest here,
     so /* an inner one */ is fine. */
  final mask = 0xff & 0b1111;
  print(mask);
}
