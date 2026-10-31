# crystal class
class Greeter
  getter name : String

  def initialize(@name)
  end

  def greet
    puts "hello, " + name
  end
end

Greeter.new("crystal").greet
