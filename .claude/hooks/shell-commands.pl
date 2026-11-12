#!/usr/bin/env perl
# Reads a Bash tool call's command line and prints the simple commands it
# runs, for branch-guard.sh, one line each:
#
#   C <TAB> flags <TAB> words   a command, its words joined by \x1f; a word
#                               known only once it runs starts with \x1d
#   (  and  )                   a subshell around the lines between them
#   R <TAB> reason              a shape the guard cannot judge; refuse it
#
# flags: p  a cd here does not outlast the command (pipeline, background)
#        x  xargs adds arguments to the command
#        g  found as `git … push` among another command's arguments
#        d  the line sets GIT_DIR or GIT_WORK_TREE
#
# Quoted text and heredoc bodies are data, except where a shell runs them:
# `bash -c`, `sh -c`, `eval`, a heredoc or here-string fed to a shell, and
# command substitutions are parsed as commands in turn. Wrappers (timeout,
# env, nohup, xargs, …) are looked through to the command they run.
#
# This guards against an agent's mistake, not against someone set on getting
# past it: what cannot be read off the text (a command name in a variable, a
# script piped into a shell) is refused when the line mentions git, push or
# commit at all, and otherwise let through.
use strict;
use warnings;

my $orig = do { local $/; <STDIN> } // '';
my $mentions_git = $orig =~ /git|push|commit/;
# An exported GIT_DIR lasts for every command after it, so any mention counts.
my $git_dir = $orig =~ /\bGIT_(?:DIR|WORK_TREE)\s*=/ ? 'd' : '';

my %WRAP = map { $_ => 1 } qw(time command builtin exec nohup nice ionice chrt
  taskset stdbuf env sudo doas setsid timeout xargs flock unbuffer caffeinate);
my %SHELL = map { $_ => 1 } qw(bash sh zsh dash ksh mksh ash);
my %RESERVED = map { $_ => 1 } qw(! { } do done then else elif fi if while until esac);
my %HEAD = (%WRAP, %SHELL, map { $_ => 1 } qw(git gh cd pushd popd eval));
# Commands whose arguments are only ever text, so `echo git push` stays an echo.
my %DATA = map { $_ => 1 } qw(echo printf grep egrep fgrep rg ag cat less more
  head tail man help type which whatis apropos jq);

our ($s, $p) = ('', 0);
our @heredocs;
my @out;
our $depth = 0;

sub ch { my ($i) = @_; $i < length $s ? substr($s, $i, 1) : '' }

sub parse_text {
  my ($text) = @_;
  local $s = $text;
  local $p = 0;
  local @heredocs;
  return parse_list(undef);
}

# Commands up to $end (a closing parenthesis) or the end of the text.
sub parse_list {
  my ($end) = @_;
  my (@entries, @words, @pre, @here);
  my $after = '';
  my $flush = sub {
    my ($sep) = @_;
    push @entries, { w => [@words], pre => [@pre], here => [@here], sep => $sep, after => $after }
      if @words || @pre;
    @words = (); @pre = (); @here = ();
    $after = $sep;
  };
  while ($p < length $s) {
    my $c = ch($p);
    if (defined $end && $c eq $end) { $p++; last }
    if ($c eq ' ' || $c eq "\t") { $p++; next }
    if ($c eq "\\" && ch($p + 1) eq "\n") { $p += 2; next }
    if ($c eq "\n") { $p++; $flush->(';'); heredoc_bodies(); next }
    if ($c eq '#') { $p++ while $p < length $s && ch($p) ne "\n"; next }
    if (($c eq '<' || $c eq '>') && ch($p + 1) eq '(') {
      $p += 2;
      push @pre, { group => parse_list(')') };
      next;
    }
    if (substr($s, $p) =~ /^(\d*)(<<<|<<-|<<|&>>|&>|>>|>&|<&|<>|>\||<|>)/) {
      my $op = $2;
      $p += length($1) + length($2);
      $p++ while ch($p) eq ' ' || ch($p) eq "\t";
      my $target = read_word(\@pre);
      if ($op eq '<<' || $op eq '<<-') {
        my $body = '';
        push @heredocs, [$target->[0], \$body];
        push @here, \$body;
      } elsif ($op eq '<<<') {
        my $body = $target->[0];
        push @here, \$body;
      }
      next;
    }
    if (substr($s, $p) =~ /^(&&|\|\||;;|\|&|;|&|\|)/) {
      $p += length $1;
      $flush->($1);
      next;
    }
    if ($c eq '(') {
      $p++;
      my $body = parse_list(')');
      # `name() { …; }` defines a function; its body follows as commands.
      push @entries, { group => $body } unless @words;
      next;
    }
    if ($c eq ')') { $p++; next }
    push @words, read_word(\@pre);
  }
  $flush->('');
  return \@entries;
}

sub heredoc_bodies {
  while (my $h = shift @heredocs) {
    my ($delim, $body) = @$h;
    my $from = $p;
    my $found = 0;
    while ($p < length $s) {
      my $e = index($s, "\n", $p);
      $e = length $s if $e < 0;
      my $line = substr($s, $p, $e - $p);
      if ($line =~ /^\s*\Q$delim\E\s*$/) { $p = $e + 1; $found = 1; last }
      # Bash also ends a heredoc inside $( … ) at "EOF)".
      if ($line =~ /^(\s*\Q$delim\E)\)/) { $p += length $1; $found = 1; last }
      $$body .= "$line\n";
      $p = $e + 1;
    }
    $p = length $s if !$found;
  }
}

# One word, its quotes removed: [value, known only at run time].
sub read_word {
  my ($pre) = @_;
  my ($v, $dyn) = ('', 0);
  if (ch($p) eq '~' && ch($p + 1) =~ m{^(?:/|\s|)$}) { $v = $ENV{HOME} // '~'; $p++ }
  while ($p < length $s) {
    my $c = ch($p);
    last if $c =~ /[\s;&|()<>]/;
    if ($c eq "\\") {
      $v .= ch($p + 1) unless ch($p + 1) eq "\n";
      $p += 2;
      next;
    }
    if ($c eq "'") {
      my $e = index($s, "'", $p + 1);
      $e = length $s if $e < 0;
      $v .= substr($s, $p + 1, $e - $p - 1);
      $p = $e + 1;
      next;
    }
    if ($c eq '$' && ch($p + 1) eq "'") {
      $p += 2;
      while ($p < length $s && ch($p) ne "'") {
        if (ch($p) eq "\\") {
          my $n = ch($p + 1);
          $v .= $n eq 'n' ? "\n" : $n eq 't' ? "\t" : $n;
          $p += 2;
          next;
        }
        $v .= ch($p++);
      }
      $p++;
      next;
    }
    if ($c eq '"') {
      $p++;
      while ($p < length $s) {
        my $d = ch($p);
        if ($d eq '"') { $p++; last }
        if ($d eq "\\") {
          my $n = ch($p + 1);
          if ($n =~ /^[\$`"\\\n]$/) { $v .= $n unless $n eq "\n"; $p += 2 }
          else { $v .= $d; $p++ }
          next;
        }
        if (($d eq '$' || $d eq '`') && expansion($pre, \$v)) { $dyn = 1; next }
        $v .= $d;
        $p++;
      }
      next;
    }
    if (($c eq '$' || $c eq '`') && expansion($pre, \$v)) { $dyn = 1; next }
    $v .= $c;
    $p++;
  }
  return [$v, $dyn];
}

# An expansion at $p: appends its text as written to $$v and returns true;
# the commands of a command substitution go to @$pre.
sub expansion {
  my ($pre, $v) = @_;
  my $start = $p;
  if (ch($p) eq '`') {
    my $e = $p + 1;
    while ($e < length $s && ch($e) ne '`') { $e += ch($e) eq "\\" ? 2 : 1 }
    (my $inner = substr($s, $p + 1, $e - $p - 1)) =~ s/\\([\$`\\])/$1/g;
    $p = $e + 1;
    push @$pre, { group => parse_text($inner) };
  } elsif (ch($p + 1) eq '(' && ch($p + 2) eq '(') {
    $p += 1;
    skip_balanced('(', ')');
  } elsif (ch($p + 1) eq '(') {
    $p += 2;
    push @$pre, { group => parse_list(')') };
  } elsif (ch($p + 1) eq '{') {
    $p += 1;
    skip_balanced('{', '}');
  } elsif (ch($p + 1) =~ /^[A-Za-z_]$/) {
    $p += 2;
    $p++ while ch($p) =~ /^\w$/;
  } elsif (ch($p + 1) =~ /^[0-9@*#?\$!-]$/) {
    $p += 2;
  } else {
    return 0;
  }
  $$v .= substr($s, $start, $p - $start);
  return 1;
}

sub skip_balanced {
  my ($open, $close) = @_;
  my $n = 0;
  while ($p < length $s) {
    my $c = ch($p++);
    $n++ if $c eq $open;
    $n-- if $c eq $close;
    last if $n == 0;
  }
}

sub base { my ($w) = @_; (my $b = $w) =~ s{^.*/}{}; $b }

sub line {
  my ($flags, @w) = @_;
  push @out, "C\t-$flags\t" . join("\x1f", map {
    (my $v = $_->[0]) =~ s/[\n\r\x1f\x1d]/ /g;
    ($_->[1] ? "\x1d" : '') . $v;
  } @w);
}

sub refuse { push @out, "R\t-\t$_[0]" }

sub nested {
  my ($text, $flags) = @_;
  if ($depth >= 8) { refuse('Shells nested this deep are not followed'); return }
  local $depth = $depth + 1;
  push @out, '(';
  emit(parse_text($text), $flags);
  push @out, ')';
}

sub emit {
  my ($entries, $inherit) = @_;
  for my $e (@$entries) {
    if ($e->{group}) {
      push @out, '(';
      emit($e->{group}, $inherit);
      push @out, ')';
      next;
    }
    if (@{ $e->{pre} }) {
      push @out, '(';
      emit($e->{pre}, $inherit);
      push @out, ')';
    }
    my $flags = $inherit;
    $flags .= 'p' if $e->{sep} =~ /^(\||\|&|&)$/ || $e->{after} =~ /^(\||\|&)$/;
    command($e, $e->{w}, $flags);
  }
}

sub command {
  my ($e, $words, $flags) = @_;
  my @w = @$words;
  while (@w && !$w[0][1] && ($RESERVED{ $w[0][0] } || $w[0][0] =~ /^[A-Za-z_]\w*\+?=/)) {
    shift @w;
  }
  return unless @w;
  if ($w[0][1]) {
    refuse("The command name $w[0][0] is only known when it runs") if $mentions_git;
    return;
  }
  my $name = base($w[0][0]);
  $name = $w[0][0] unless $HEAD{$name};
  return if $name =~ /^(?:for|case|select|function)$/;

  if ($WRAP{$name}) {
    $flags .= 'x' if $name eq 'xargs';
    my $chdir;
    for my $j (1 .. $#w) {
      my ($u, $ud) = @{ $w[$j] };
      next if $ud;
      if ($name eq 'env') {
        if ($u =~ /^(?:-C|--chdir)$/) { $chdir = $w[$j + 1]; next }
        if ($u =~ /^(?:-C|--chdir=)(.+)$/) { $chdir = [$1, 0]; next }
      }
      next unless $HEAD{ base($u) };
      my @rest = @w[$j .. $#w];
      if ($chdir) {
        push @out, '(';
        line('', ['cd', 0], $chdir);
        command($e, \@rest, $flags);
        push @out, ')';
      } else {
        command($e, \@rest, $flags);
      }
      return;
    }
    return;
  }

  if ($SHELL{$name}) {
    for my $j (1 .. $#w) {
      my ($u, $ud) = @{ $w[$j] };
      return if $ud || $u !~ /^[-+]/;    # a script file: not ours to read
      if ($u =~ /^-[A-Za-z]*c[A-Za-z]*$/) {
        nested($w[$j + 1][0], $flags) if $j < $#w;
        return;
      }
    }
    if (@{ $e->{here} }) {
      nested($$_, $flags) for @{ $e->{here} };
    } elsif ($mentions_git) {
      refuse("A shell reading its commands from a pipe or a file cannot be judged");
    }
    return;
  }

  if ($name eq 'eval') {
    nested(join(' ', map { $_->[0] } @w[1 .. $#w]), $flags);
    return;
  }

  if ($name =~ /^(?:git|gh|cd|pushd|popd)$/) {
    line($flags, [$name, 0], @w[1 .. $#w]);
    return;
  }

  return if $DATA{$name};
  # find -exec, watch, parallel and the like run their arguments.
  for my $j (1 .. $#w - 1) {
    next if $w[$j][1] || base($w[$j][0]) ne 'git';
    next unless grep { !$_->[1] && $_->[0] eq 'push' } @w[$j + 1 .. $#w];
    line($flags . 'g', ['git', 0], @w[$j + 1 .. $#w]);
    return;
  }
}

$s = $orig;
emit(parse_list(undef), $git_dir);
print "$_\n" for @out;
