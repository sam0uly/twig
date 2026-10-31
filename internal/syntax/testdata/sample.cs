using System;
using System.Collections.Generic;

namespace Twig;

/// <summary>Caches recent entries.</summary>
public sealed class Cache
{
    private readonly List<string> _slots = new();

    public void Add(string key) => _slots.Add(key);

    public bool Contains(string key) => _slots.Contains(key);

    // Verbatim strings keep their backslashes; raw strings keep their quotes.
    public static string Path() => @"C:\temp\logs";

    public static string Json() => """
        {
          "kind": "cache",
          "capacity": 16
        }
        """;

    /* Numbers in several bases:
       hex, binary and a decimal float. */
    public static int Mask() => 0xff & 0b1111;

    public static double Ratio() => 1.5e3;
}
