use std::collections::HashMap;

/// A doc comment with a "quote" inside.
pub struct Cache<'a> {
    entries: HashMap<&'a str, u64>,
    limit: usize,
}

impl<'a> Cache<'a> {
    /* a block comment
       over two lines */
    pub fn new(limit: usize) -> Self {
        let total = 0x1f + 3.5e2;
        println!("made {total} entries");
        Self { entries: HashMap::new(), limit }
    }
}
