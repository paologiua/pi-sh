# pi-sh forwarding adapter, Perl data path. forward.bash hands the carrier to
# this program (exec, nothing written) when Go asks for it with "P" and Perl
# compiles it; it then speaks the same records as forward.bash (mux.go), so a
# device without Perl keeps the Bash loop. Only perl-base modules are used.
# This text is embedded in a single-quoted Bash word: it holds no apostrophe.
use strict;
use warnings;
use POSIX ();
use Fcntl qw(F_GETFL F_SETFL O_NONBLOCK);

my ($P, $W, $relay) = @ARGV;
$W =~ /\A[1-9][0-9]{0,3}\z/ or exit 125;
$SIG{PIPE} = "IGNORE";
my %s;
my ($out, $in) = ("", "");
# Longest record: an open of maxOpen (4224) bytes after its two-byte head.
my $limit = length($P) + 4226;

sub emit { $out .= $P . $_[0] . "\n" }
sub flush {
  while (length $out) {
    my $n = syswrite(STDOUT, $out);
    if (!defined $n) { next if $!{EINTR}; cleanup(); exit 125 }
    substr($out, 0, $n) = "";
  }
}
sub nonblock { my $f = shift; fcntl($f, F_SETFL, fcntl($f, F_GETFL, 0) | O_NONBLOCK) }
sub kill_stream { my $k = shift; kill("KILL", $k->{pid}) if !$k->{reaped} }
sub drop {
  my $id = shift;
  my $k = delete $s{$id} or return;
  kill_stream($k);
  close($_) for grep { defined } @$k{qw(in out err)};
  waitpid($k->{pid}, 0) if !$k->{reaped};
}
sub cleanup {
  $SIG{HUP} = $SIG{TERM} = "IGNORE";
  drop($_) for keys %s;
  kill("TERM", $relay) if defined $relay && $relay =~ /\A[0-9]+\z/;
}
sub fail { emit("Z protocol"); cleanup(); flush(); exit 125 }
$SIG{HUP} = $SIG{TERM} = sub { cleanup(); exit 125 };

sub inclosed {
  my ($id, $k) = @_;
  $k->{iclosed} = 1; $k->{q} = {}; $k->{wbuf} = "";
  close($k->{in}) if defined $k->{in};
  $k->{in} = undef;
  emit("k $id");
}
# Hand the connector the next queued input record, or EOF once every record
# is written; a record is acknowledged when all of it was written.
sub pump {
  my ($id, $k) = @_;
  while (!$k->{iclosed}) {
    if (length $k->{wbuf}) {
      my $n = syswrite($k->{in}, $k->{wbuf});
      if (!defined $n) { return if $!{EAGAIN} || $!{EINTR}; inclosed($id, $k); return }
      substr($k->{wbuf}, 0, $n) = "";
      return if length $k->{wbuf};
      $k->{iack} = $k->{ifwd};
      emit("a $id $k->{iack}");
    } elsif ($k->{ifwd} < $k->{iseq}) {
      my $h = delete $k->{q}{++$k->{ifwd}};
      $k->{wbuf} = $k->{mode} eq "h" ? "$h\n" : pack("H*", $h);
    } elsif ($k->{ifin}) {
      close($k->{in}); $k->{in} = undef; $k->{iclosed} = 1;
      return;
    } else {
      return;
    }
  }
}

sub open_stream {
  my ($id, $mode, @w) = @_;
  defined $mode && $id =~ /\A[1-9][0-9]{0,9}\z/ && !$s{$id} && $mode =~ /\A[rh]\z/ or fail();
  keys(%s) < 64 && @w or fail();
  for (@w) { /\A(?:[0-9a-f]{2}){1,2048}\z/ or fail(); $_ = pack("H*", $_) }
  my ($ir, $iw, $or, $ow, $er, $ew);
  pipe($ir, $iw) && pipe($or, $ow) && pipe($er, $ew) or fail();
  my $pid = fork();
  defined $pid or fail();
  if (!$pid) {
    $SIG{PIPE} = "DEFAULT";
    POSIX::dup2(fileno $ir, 0); POSIX::dup2(fileno $ow, 1); POSIX::dup2(fileno $ew, 2);
    { no warnings "exec"; exec { $w[0] } @w }
    my $e = $!{ENOENT} ? "command not found" : "cannot execute";
    syswrite(STDERR, "pi-sh: $e\n");
    POSIX::_exit($!{ENOENT} ? 127 : 126);
  }
  close($ir); close($ow); close($ew);
  nonblock($_) for $iw, $or, $er;
  $s{$id} = { pid => $pid, mode => $mode, in => $iw, out => $or, err => $er, obuf => "", wbuf => "", q => {},
    oseq => 0, oack => 0, oeof => 0, eeof => 0, iseq => 0, ifwd => 0, iack => 0, ifin => 0, iclosed => 0,
    ebytes => 0, reaped => 0, grace => undef };
}

# A malformed record from a hex connector ends that stream only.
sub badout {
  my ($id, $k) = @_;
  kill_stream($k); $k->{oeof} = 1; close($k->{out}) if defined $k->{out}; $k->{out} = undef; emit("f $id");
}
sub outeof {
  my ($id, $k) = @_;
  $k->{oeof} = 1; close($k->{out}) if defined $k->{out}; $k->{out} = undef; emit("f $id");
}
# Hex connector records: "length\nhex\n" (or "F\n", as an end of output).
# oend marks the end of the pipe; queued records are still delivered first.
sub hexrecords {
  my ($id, $k) = @_;
  while (!$k->{oeof} && $k->{oseq} - $k->{oack} < $W) {
    my $b = \$k->{obuf};
    if ($k->{oend} && !length $$b) { outeof($id, $k); return }
    my $nl = index($$b, "\n");
    if ($nl < 0) { badout($id, $k) if length $$b > 5 || $k->{oend}; return }
    my $n = substr($$b, 0, $nl);
    if ($n eq "F") { outeof($id, $k); return }
    $n =~ /\A[1-9][0-9]{0,3}\z/ && $n % 2 == 0 && $n <= 4096 or return badout($id, $k);
    if (length($$b) < $nl + 2 + $n) { badout($id, $k) if $k->{oend}; return }
    my $h = substr($$b, $nl + 1, $n);
    $h =~ /\A[0-9a-f]*\z/ && substr($$b, $nl + 1 + $n, 1) eq "\n" or return badout($id, $k);
    substr($$b, 0, $nl + 2 + $n) = "";
    $k->{oseq}++;
    emit("o $id $k->{oseq} $h");
  }
}

sub record {
  my $r = shift;
  if ($r eq "Q") { cleanup(); emit("D"); flush(); exit 0 }
  if ($r =~ /\AI /) {
    my ($id, $seq, $n, $h) = $r =~ /\AI ([0-9]{1,10}) ([0-9]{1,10}) ([0-9]{1,4}) ([0-9a-f]*)\z/ or fail();
    $n > 0 && $n <= 4096 && $n % 2 == 0 && length($h) == $n or fail();
    my $k = $s{$id} or return;
    return if $k->{iclosed};
    $seq == $k->{iseq} + 1 && $k->{iseq} - $k->{iack} < $W && !$k->{ifin} or fail();
    $k->{iseq} = $seq; $k->{q}{$seq} = $h;
    pump($id, $k);
    return;
  }
  my ($t, $rest) = $r =~ /\A([OFACG]) (.*)\z/s or fail();
  length($rest) <= 4224 or fail();
  if ($t eq "O") { open_stream(split(" ", $rest)); return }
  if ($t eq "A") {
    my ($id, $seq) = $rest =~ /\A([0-9]+) ([0-9]+)\z/ or fail();
    my $k = $s{$id} or return;
    $seq == $k->{oack} + 1 && $seq <= $k->{oseq} or fail();
    $k->{oack} = $seq;
    return;
  }
  $rest =~ /\A[0-9]+\z/ or fail();
  my $id = $rest;
  my $k = $s{$id};
  if ($t eq "F") {
    return if !$k || $k->{iclosed};
    $k->{ifin} = 1; pump($id, $k);
  } elsif ($t eq "G") {
    # Both directions ended: let the connector take queued input and exit.
    if ($k) { $k->{ifin} = 1; pump($id, $k); $k->{grace} = time + 10 } else { emit("d $id") }
  } else {
    drop($id) if $k;
    emit("d $id");
  }
}

nonblock(\*STDIN);
emit("P");
flush();
while (1) {
  my ($r, $w, $wait) = ("", "", undef);
  # Streams opened while records are handled below may reuse the numbers of
  # descriptors closed there; only the streams polled now are serviced.
  my %polled = %s;
  vec($r, fileno(STDIN), 1) = 1;
  for my $k (values %s) {
    vec($r, fileno($k->{out}), 1) = 1 if defined $k->{out} && $k->{oseq} - $k->{oack} < $W && length($k->{obuf}) < 8200;
    vec($r, fileno($k->{err}), 1) = 1 if !$k->{eeof};
    vec($w, fileno($k->{in}), 1) = 1 if !$k->{iclosed} && length $k->{wbuf};
    $wait = 0.01 if $k->{oeof} && $k->{eeof};
    $wait = 1 if defined $k->{grace} && !defined $wait;
  }
  my $n = select($r, $w, undef, $wait);
  if ($n < 0) { next if $!{EINTR}; fail() }
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
  for my $id (keys %polled) {
    my $k = $s{$id};
    next if !$k || $k != $polled{$id};
    pump($id, $k) if !$k->{iclosed} && length $k->{wbuf} && vec($w, fileno($k->{in}), 1);
    if (!$k->{eeof} && vec($r, fileno($k->{err}), 1)) {
      my $got = sysread($k->{err}, my $b, 256);
      if ($got) {
        emit("e $id " . unpack("H*", $b)) if $k->{ebytes} < 4096;
        $k->{ebytes} += $got;
      } elsif (defined $got || !($!{EINTR} || $!{EAGAIN})) {
        $k->{eeof} = 1; close($k->{err}); $k->{err} = undef;
      }
    }
    if (defined $k->{out} && vec($r, fileno($k->{out}), 1)) {
      my $got = sysread($k->{out}, my $b, $k->{mode} eq "h" ? 8200 : 2048);
      if ($got) {
        if ($k->{mode} eq "h") { $k->{obuf} .= $b } else { $k->{oseq}++; emit("o $id $k->{oseq} " . unpack("H*", $b)) }
      } elsif (defined $got || !($!{EINTR} || $!{EAGAIN})) {
        if ($k->{mode} eq "h") { $k->{oend} = 1; close($k->{out}); $k->{out} = undef; hexrecords($id, $k) }
        else { outeof($id, $k) }
      }
    }
    hexrecords($id, $k) if $k->{mode} eq "h" && !$k->{oeof} && (length $k->{obuf} || $k->{oend});
    if (defined $k->{grace} && time >= $k->{grace}) { kill_stream($k); $k->{grace} = undef }
    if ($k->{oeof} && $k->{eeof} && waitpid($k->{pid}, POSIX::WNOHANG()) == $k->{pid}) {
      my $st = $?;
      $k->{reaped} = 1;
      emit("x $id " . (($st & 127) ? 128 + ($st & 127) : $st >> 8));
      drop($id);
    }
  }
  flush();
}
