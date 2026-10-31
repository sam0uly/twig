#!/usr/bin/perl
use strict;
use warnings;

# A bounded cache of recent keys.

my $DEFAULT = 16;

sub new_cache {
    my ($class, %opts) = @_;
    my $capacity = $opts{capacity} // $DEFAULT;
    return bless { capacity => $capacity, slots => [] }, $class;
}

sub push {
    my ($self, $key) = @_;
    shift @{ $self->{slots} } if @{ $self->{slots} } >= $self->{capacity};
    unshift @{ $self->{slots} }, $key;
}

sub report {
    my ($self) = @_;
    my $body = <<"JSON";
{"kind": "cache", "size": @{ [ @{ $self->{slots} } ] }}
JSON
    return $body;
}

# Numbers in several bases.
my $mask = 0xff & 0b1111;
my $ratio = 1.5e3;

print new_cache()->report() . $mask . $ratio;
