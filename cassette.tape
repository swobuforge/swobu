Output assets/demo.gif

Set Shell zsh
Set Width 960
Set Height 540
Set FontSize 14
Set LineHeight 1.25
Set WindowBar Colorful
Set Theme "GitHub Dark"
Set Framerate 15
Set TypingSpeed 35ms
Set Padding 0
Set LoopOffset 10

# Start Swobu
Copy "go run ./cmd/swobu"
Paste
Enter

Sleep 1.5s
Enter
Sleep 3s

# Navigate / enter name
Down
Enter
Sleep 3s
Type "test"
Sleep 500ms
Type "name"
Enter
Sleep 500ms

# Navigate menu
Down
Enter
Sleep 500ms
Enter

Up
Sleep 1s
Down
Enter

Down
Sleep 500ms
Enter

Down 3
Enter
Sleep 500ms

Down
Sleep 500ms
Enter
Sleep 1s

Enter
Sleep 500ms
Type "o"
Sleep 500ms
Type "pe"

Down 2
Enter
Sleep 500ms

Down
Enter
Sleep 2s

Type "turb"
Sleep 1s
Enter
Sleep 1s

Up 5
Sleep 1.5s
Enter
Sleep 500ms

Down 4
Sleep 1s
Enter 2

Down
Sleep 500ms
Enter
Sleep 1s

Down
Sleep 500ms
Enter
Sleep 500ms

# Suspicious terminal capability hack.
# Delete this if the app works without it.
Escape
Type "P>|tmux 3.4"
Escape
Type "\"
Escape

Sleep 1s
Type "Hello World!"
Enter
Sleep 2.5s

Ctrl+C
Ctrl+C
Sleep 1s

Down 2
Enter
Up
Sleep 5s

Ctrl+C
Sleep 500ms