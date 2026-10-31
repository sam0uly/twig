# frozen_string_literal: true

require "json"

module Twig
  # A bounded cache of recent keys.
  class Cache
    attr_reader :slots

    def initialize(capacity: 16)
      @capacity = capacity
      @slots = []
    end

    def push(key)
      @slots.shift if @slots.size >= @capacity
      @slots << key
    end

    def include?(key) = @slots.include?(key)

    def report
      <<~JSON
        {"kind": "cache", "size": #{@slots.size}}
      JSON
    end
  end
end

=begin
A block comment also works this way,
though it is used less often.
=end

mask = 0xff & 0b1111
ratio = 1.5e3
puts Twig::Cache.new.report + mask.to_s + ratio.to_s
