# Trabajo en tmux-sentinela

Al terminar un cambio de Go que afecte al binario de Sentinela:

1. Ejecutar los tests antes de desplegar.
2. Compilar en un archivo temporal dentro de `bin/` y reemplazar `bin/tmux-sentinela` mediante `mv` sólo si compiló bien. El reemplazo del ejecutable activa el reinicio automático de las sidebars existentes en los siguientes ciclos de sondeo; `refresh` por sí solo no carga código nuevo.
3. Si hay sidebars abiertas, comprobar que todas ejecutan el binario nuevo antes de dar el trabajo por terminado. En Linux, comparar el inode de `/proc/<pane_pid>/exe` con el de `bin/tmux-sentinela`, usando los pane PID que informa tmux. Esperar el reinicio automático un tiempo acotado; si alguna no se actualiza, comunicar cuáles quedaron pendientes.

No cerrar ni recrear sidebars para una recompilación habitual: el reinicio automático conserva sus panes y el foco.
