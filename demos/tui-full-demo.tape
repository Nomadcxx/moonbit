Output tui-full.gif

Set FontSize 18
Set Width 1920
Set Height 1080
Set Theme "Catppuccin Mocha"
Set PlaybackSpeed 1.5

Type "# MoonBit TUI - Interactive System Cleaner"
Enter
Sleep 800ms

Type "/home/nomadx/Documents/moonbit/moonbit-demo/moonbit-demo"
Enter
Sleep 1.5s

# Show welcome screen with ASCII
Sleep 2s

# Navigate menu
Down
Sleep 600ms
Down
Sleep 600ms

# Go back to Quick Scan
Up
Sleep 600ms

# Select Quick Scan
Enter
Sleep 1s

# Wait for scan to complete
Sleep 8s

# Now in category selection - navigate categories
Down
Sleep 500ms
Down
Sleep 500ms
Down
Sleep 500ms

# Go back up
Up@2
Sleep 600ms

# Toggle selection with Space
Space
Sleep 400ms

# Navigate to another category
Down@4
Sleep 700ms

# Toggle another
Space
Sleep 400ms

# Show the selections
Sleep 1.5s

# Navigate to buttons (use arrow keys)
Right
Sleep 400ms

# Select Clean
Enter
Sleep 1s

# Show cleaning in progress
Sleep 5s

# Exit
Type "q"
Sleep 800ms
