# Commands in the MUD

## What is a command
A command is a block of executable code that runs something a player might 
want to do.  For example, a player may want to say any of these things and 
have the system act on them:
* Look
* Go east/west/north/south
* Pick up <item>

In these examples, we'd have 3 commands - look, go and pick-up.

# Commands in code
I want each command to be an object with an execute function.  The execute 
function takes as session ID, so that we can tie commands together into a 
conversation.  The commands should return a string data type, which 
represent the output of the command.

Commands should have a function `match_command` that takes in a string and 
returns whether this command can be used. It does not run the command, it 
simply checks for a match.  

Commands should also have a function `run_command` which takes the same 
string and runs it.  

# Command-runner
There should be a command runner.  The telnet server will send each line to 
the command runner and expect a string back to display. 

When the command runner gets a string from the telnet server, it should run 
`match_command` on every command to see if it matches.  Then, if it matches, 
it should run the `run_command` function.

# Command implementations

Implement the `look` command (which takes no input, so the `run_command` 
function can ignore the input parameters), and the `go` command, which can 
simply return "lorem ipsum"

# Telnet Server updates

The telnet server needs to be changed to support commands.  When a new 
connection is made, it should generate a new session ID (a random uuid is a 
good fit).  Every line should get passed to the command runner, and the 
response printed to the output stream