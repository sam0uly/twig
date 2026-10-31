#include <array>
#include <optional>
#include <string>

namespace twig {

// A small ring of recent entries.
template <typename T, std::size_t N>
class Ring {
public:
    void push(const T& value) {
        slots_[(head_++) % N] = value;
    }

    std::optional<T> back() const {
        if (head_ == 0) return std::nullopt;
        return slots_[(head_ - 1) % N];
    }

private:
    std::array<T, N> slots_{};
    std::size_t head_ = 0;
};

}  // namespace twig

int main() {
    twig::Ring<std::string, 16> ring;
    ring.push("hello");
    const auto raw = R"json({"ok": true, "n": 3})json";
    auto mask = 0xffu;  // bit mask
    auto bits = 0b1010;
    return ring.back() ? 0 : 1;
}
