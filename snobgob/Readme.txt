This is just a copy of the standard encoding/gob library, except it has been extended with 
a very small change in decode.go that let's it assign values to interfaces where the 
interface is only satisfied by the pointer type.

search for "oliver's edit" in decode.go