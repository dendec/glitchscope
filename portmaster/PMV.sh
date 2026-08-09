#!/bin/bash

XDG_DATA_HOME=${XDG_DATA_HOME:-$HOME/.local/share}

if [ -d "/opt/system/Tools/PortMaster/" ]; then
  controlfolder="/opt/system/Tools/PortMaster"
elif [ -d "/opt/tools/PortMaster/" ]; then
  controlfolder="/opt/tools/PortMaster"
elif [ -d "$XDG_DATA_HOME/PortMaster/" ]; then
  controlfolder="$XDG_DATA_HOME/PortMaster"
else
  controlfolder="/roms/ports/PortMaster"
fi

source $controlfolder/control.txt
[ -f "${controlfolder}/mod_${CFW_NAME}.txt" ] && source "${controlfolder}/mod_${CFW_NAME}.txt"
get_controls

GAMEDIR="/$directory/ports/pmv"
CONFDIR="$GAMEDIR/conf/"

mkdir -p "$GAMEDIR/conf"
cd "$GAMEDIR"

> "$GAMEDIR/log.txt" && exec > >(tee "$GAMEDIR/log.txt") 2>&1

export XDG_DATA_HOME="$CONFDIR"

export LD_LIBRARY_PATH="$GAMEDIR/lib:/usr/lib:$LD_LIBRARY_PATH"
export SDL_GAMECONTROLLERCONFIG="$sdl_controllerconfig"
export SDL_AUDIODRIVER="alsa"

# The handheld's codec exposes its capture PCM with the MIC input route muted
# after boot. Enable it when these codec controls are available; other devices
# simply ignore the unsupported controls.
amixer -c 0 cset numid=12 on >/dev/null 2>&1 || true
amixer -c 0 cset numid=13 on >/dev/null 2>&1 || true
amixer -c 0 cset numid=9 160,160 >/dev/null 2>&1 || true

# Display standard PortMaster loading text
pm_message "Loading PMV... (Compiling shaders)"

# gptokeyb handles START+SELECT quit combo; kills pmv on combo press
$GPTOKEYB "pmv" &

pm_platform_helper "$GAMEDIR/pmv"
./pmv -fullscreen -show-fps

pm_finish
