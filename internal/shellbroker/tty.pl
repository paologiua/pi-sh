# Independently authored pi-sh TTY supervisor, Perl implementation. tty.bash
# sets the terminal modes with stty, then execs this program when perl -c
# compiles it; tty.bash keeps its own supervisor as the fallback. Same pid and
# process group as the interactive Bash that set them up (the carrier group,
# in the foreground), so job control is unchanged: the command runs in this
# group, and Ctrl-Z stops the command, never this supervisor. Only perl-base
# modules are used. This text is embedded in a single-quoted Bash word: it
# holds no apostrophe (TestTTYPerlHasNoApostrophe).
use strict;
use warnings;
use POSIX ();

my ($P, $shell, $lcset, $oldlc, $command) = @ARGV;
defined $command or exit 125;
my $owner = $$;
# Never let PID 1 become the special process selector -1 of kill.
$owner > 1 or exit 125;
my ($child, $complete) = (undef, 0);
# exec in tty.bash restored the dispositions of its entry; the command gets
# them back, as the commands of the interactive Bash did.
my @kept = qw(HUP TERM INT QUIT PIPE CHLD);
my %initial = map { $_ => (defined $SIG{$_} && $SIG{$_} eq "IGNORE" ? "IGNORE" : "DEFAULT") } @kept;
# As _pt_abort: a job-control shell (a login shell) leaves this group, so like
# the session leader of sshd it gets HUP and CONT, which it resends to its jobs;
# then the whole carrier group goes, this supervisor included.
sub abort {
  $SIG{$_} = "IGNORE" for qw(HUP TERM);
  if (defined $child && $child > 1) { kill("HUP", $child); kill("CONT", $child) }
  kill("KILL", -$owner, $owner);
  POSIX::_exit(125);
}
$SIG{HUP} = $SIG{TERM} = \&abort;
# Terminal signals go to the whole foreground group: the command receives
# them; this supervisor handles INT and QUIT (handled, not ignored, so the
# command starts with the default action) and ignores the stops, as the
# interactive Bash did.
$SIG{INT} = $SIG{QUIT} = sub { };
$SIG{$_} = "IGNORE" for qw(TSTP TTIN TTOU);

open(my $tty, "+<", "/dev/tty") or abort();
syswrite($tty, $P . "TR\037") or abort();
$child = fork();
defined $child or abort();
if (!$child) {
  # The interactive Bash gave its commands default job-control signals,
  # whatever it inherited; the others as they were before this program.
  $SIG{$_} = $initial{$_} for @kept;
  $SIG{$_} = "DEFAULT" for qw(TSTP TTIN TTOU);
  if ($lcset eq "x") { $ENV{LC_ALL} = $oldlc } else { delete $ENV{LC_ALL} }
  $ENV{SHLVL}++ if defined $ENV{SHLVL} && $ENV{SHLVL} =~ /\A-?[0-9]{1,9}\z/;
  POSIX::dup2(fileno $tty, $_) for 0, 1, 2;
  { no warnings "exec"; exec { $shell } $shell, "-c", $command }
  syswrite(STDERR, "$shell: $!\n");
  POSIX::_exit($!{ENOENT} ? 127 : 126);
}
# waitpid does not report stops: a stopped command is still awaited.
my $r;
1 while ($r = waitpid($child, 0)) < 0 && $!{EINTR};
$r == $child or abort();
my $status = $?;
$child = undef;
my $code = ($status & 127) ? 255 : $status >> 8;
# Result framing follows classification and predictable modes.
system("stty", "raw", "-echo") == 0 or abort();
$complete = 1;
syswrite($tty, $P . "TX:" . $code . "\037");
exit $code;
