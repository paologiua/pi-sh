# Independently authored pi-sh command broker, Perl implementation. broker.bash
# execs it after perl -c compiles it; it speaks exactly the records of the Bash
# broker (broker.go), which stays as the fallback on a device without Perl.
# One process: no coprocesses, relays or workers, and one blocking select
# instead of polling. Only perl-base modules are used. This text is embedded
# in a single-quoted Bash word: it holds no apostrophe (TestBrokerPerlHasNoApostrophe).
use strict;
use warnings;
use POSIX ();
use Fcntl qw(F_GETFL F_SETFL O_NONBLOCK);

my ($P, $inwin, $outwin, $shell, $lcset, $oldlc, $command) = @ARGV;
defined $command && $inwin =~ /\A[1-9][0-9]{0,3}\z/ && $outwin =~ /\A[1-9][0-9]{0,3}\z/ or exit 125;
# The command gets the dispositions this program started with (broker.bash
# runs it with those of its own entry: exec restores them).
my @touched = qw(PIPE HUP TERM CHLD INT QUIT);
my %initial = map { $_ => (defined $SIG{$_} && $SIG{$_} eq "IGNORE" ? "IGNORE" : "DEFAULT") } @touched;
$SIG{PIPE} = "IGNORE";
my $out = "";
# Longest record: "I seq length hex" with 4096 hex digits, within maxRecord.
my $limit = length($P) + 4226;
my ($pid, $status, $done) = (undef, undef, 0);

sub emit { $out .= $P . $_[0] . "\n" }
# The carrier is written without blocking and read in every round, even
# while output waits: blocked on output, this broker would stop taking input,
# the client could block sending it, and neither would move again. The Bash
# broker relied on its relay pipe (64 KiB) to absorb that input instead
# (SHELL_BROKER.md "Input window"). Pending output is bounded by the windows.
sub push_out {
  while (length $out) {
    my $n = syswrite(STDOUT, $out);
    if (!defined $n) { return if $!{EINTR} || $!{EAGAIN}; cleanup(); exit 125 }
    substr($out, 0, $n) = "";
  }
}
# Deliver what is pending, still draining the carrier, then exit.
sub finish {
  my $code = shift;
  while (length $out) {
    my ($r, $w) = ("", "");
    vec($r, fileno(STDIN), 1) = 1;
    vec($w, fileno(STDOUT), 1) = 1;
    if (select($r, $w, undef, undef) < 0) { next if $!{EINTR}; last }
    if (vec($r, fileno(STDIN), 1)) {
      my $got = sysread(STDIN, my $discard, 65536);
      last if defined $got ? $got == 0 : !($!{EAGAIN} || $!{EINTR});
    }
    push_out() if vec($w, fileno(STDOUT), 1);
  }
  exit $code;
}
sub nonblock { my $f = shift; fcntl($f, F_SETFL, fcntl($f, F_GETFL, 0) | O_NONBLOCK) }
# As _ps_cleanup: before a completion was reported, kill the command group
# (and the command itself, should setsid not have run yet), then reap it.
sub cleanup {
  $SIG{HUP} = $SIG{TERM} = "IGNORE";
  return if $done || !defined $pid;
  kill("KILL", -$pid); kill("KILL", $pid); kill("KILL", -$pid);
  if (!defined $status) { 1 while waitpid($pid, 0) < 0 && $!{EINTR}; $status = $? }
  # Descendants take KILL asynchronously: confirm only once the group is gone
  # (or only zombies, which init reaps, remain), within one second.
  for (1 .. 100) { last if !kill(0, -$pid); select(undef, undef, undef, 0.01) }
}
sub fail { emit("Z protocol"); cleanup(); finish(125) }
$SIG{HUP} = $SIG{TERM} = sub { cleanup(); exit 125 };
# A child exit interrupts select; a wakeup lost to deferred signal delivery
# is bounded by the timeout used while only the exit is awaited.
$SIG{CHLD} = sub { };

my ($cin, $cinw, $cout, $coutw, $cerr, $cerrw);
pipe($cin, $cinw) && pipe($cout, $coutw) && pipe($cerr, $cerrw) or exit 125;
$pid = fork();
defined $pid or exit 125;
if (!$pid) {
  POSIX::setsid();
  $SIG{$_} = $initial{$_} for @touched;
  if ($lcset eq "x") { $ENV{LC_ALL} = $oldlc } else { delete $ENV{LC_ALL} }
  # exec in broker.bash lowered SHLVL by one; the Bash broker passes it on
  # unchanged, so restore it for the command.
  $ENV{SHLVL}++ if defined $ENV{SHLVL} && $ENV{SHLVL} =~ /\A-?[0-9]{1,9}\z/;
  POSIX::dup2(fileno $cin, 0); POSIX::dup2(fileno $coutw, 1); POSIX::dup2(fileno $cerrw, 2);
  { no warnings "exec"; exec { $shell } $shell, "-c", $command }
  my $e = $!{ENOENT} ? 127 : 126;
  syswrite(STDERR, "$shell: $!\n");
  POSIX::_exit($e);
}
close($cin); close($coutw); close($cerrw);
nonblock($_) for $cinw, $cout, $cerr, \*STDIN, \*STDOUT;

my %o = (O => { fh => $cout, seq => 0, ack => 0, eof => 0 }, E => { fh => $cerr, seq => 0, ack => 0, eof => 0 });
# Input: in_seq received, in_fwd taken from the queue, in_ack written; wbuf
# holds the rest of the record being written (input_pending in broker.bash).
my ($in_seq, $in_fwd, $in_ack, $closed) = (0, 0, 0, 0);
my (%q, $wbuf, $in);
$in = "";

sub input_closed {
  $closed = 1; $wbuf = undef; $in_fwd = $in_seq; %q = ();
  close($cinw) if defined $cinw;
  $cinw = undef;
  emit("K");
}
# The command receives one record at a time; A follows its last byte.
sub pump {
  while (!$closed) {
    if (defined $wbuf) {
      my $n = syswrite($cinw, $wbuf);
      if (!defined $n) { return if $!{EAGAIN} || $!{EINTR}; input_closed(); return }
      substr($wbuf, 0, $n) = "";
      return if length $wbuf;
      $wbuf = undef; $in_ack = $in_fwd;
      emit("A $in_fwd");
    } elsif ($in_fwd < $in_seq) {
      $wbuf = pack("H*", delete $q{++$in_fwd});
    } else {
      return;
    }
  }
}

sub record {
  my $r = shift;
  if ($r =~ /\AA ([OE]) ([0-9]{1,22})\z/) {
    my $s = $o{$1};
    $2 == $s->{ack} + 1 && $2 <= $s->{seq} or fail();
    $s->{ack} = $2;
  } elsif ($r =~ /\AI ([0-9]{1,9}) ([0-9]{1,4}) ([0-9a-f]*)\z/) {
    my ($seq, $n, $h) = ($1, $2, $3);
    $seq == $in_seq + 1 && $n > 0 && $n <= 4096 && $n % 2 == 0 && $in_seq - $in_ack < $inwin && length($h) == $n or fail();
    $in_seq = $seq;
    # Records arriving after K are validated and dropped.
    if (!$closed) { $q{$seq} = $h; pump() }
  } elsif ($r eq "F") {
    !defined $wbuf && $in_fwd == $in_seq or fail();
    if (!$closed) { $closed = 1; close($cinw); $cinw = undef }
  } elsif ($r eq "C") {
    # D is a pi-sh application acknowledgement, never native agent status.
    cleanup(); $done = 1; emit("D"); finish(125);
  } else {
    fail();
  }
}

emit("R");
while (1) {
  my ($r, $w) = ("", "");
  vec($r, fileno(STDIN), 1) = 1;
  my @polled;
  for my $name ("O", "E") {
    my $s = $o{$name};
    next if $s->{eof} || $s->{seq} - $s->{ack} >= $outwin;
    vec($r, fileno($s->{fh}), 1) = 1;
    push @polled, $name;
  }
  vec($w, fileno(STDOUT), 1) = 1 if length $out;
  my $writing = !$closed && defined $wbuf;
  vec($w, fileno($cinw), 1) = 1 if $writing;
  my $ended = $o{O}{eof} && $o{E}{eof};
  my $n = select($r, $w, undef, $ended && !defined $status ? 0.1 : undef);
  if ($n < 0) { fail() if !$!{EINTR}; $r = $w = "" }
  # Only descriptors polled in this round are serviced: handling records can
  # close one, and a new descriptor could reuse its number.
  pump() if $writing && !$closed && vec($w, fileno($cinw), 1);
  for my $name (@polled) {
    my $s = $o{$name};
    next if !vec($r, fileno($s->{fh}), 1);
    my $got = sysread($s->{fh}, my $buf, 2048);
    if ($got) {
      $s->{seq}++;
      emit("$name $s->{seq} " . unpack("H*", $buf));
    } elsif (defined $got || !($!{EINTR} || $!{EAGAIN})) {
      $s->{eof} = 1; close($s->{fh}); emit("F $name");
    }
  }
  if (vec($r, fileno(STDIN), 1)) {
    my $got = sysread(STDIN, $in, 65536, length $in);
    if (defined $got && $got == 0) { cleanup(); exit 125 }
    if (!defined $got && !$!{EAGAIN} && !$!{EINTR}) { cleanup(); exit 125 }
    while ((my $nl = index($in, "\n")) >= 0) {
      my $line = substr($in, 0, $nl + 1, "");
      chop $line;
      substr($line, 0, length $P) eq $P or fail();
      record(substr($line, length $P));
    }
    fail() if length($in) > $limit;
  }
  if (!defined $status && waitpid($pid, POSIX::WNOHANG()) == $pid) { $status = $? }
  if (defined $status && $o{O}{eof} && $o{E}{eof} && $o{O}{seq} == $o{O}{ack} && $o{E}{seq} == $o{E}{ack}) {
    $done = 1;
    emit(($status & 127) ? "Y " . ($status & 127) : "X " . ($status >> 8));
    finish(0);
  }
  push_out();
}
