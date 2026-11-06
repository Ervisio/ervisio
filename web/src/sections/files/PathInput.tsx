import { useEffect, useRef } from 'react';
import { errorMessage, useSession } from '../../api';
import { useT } from '../../i18n';
import { IconButton, Input, toast } from '../../ui';
import type { InputProps } from '../../ui/Form';
import { requestFilePicker } from './pickerService';
import { dirname } from './util';

/** A manual path remains pasteable; Browse always invokes the shared chooser. */
export function PathInput({value,onPathChange,mode='file',...props}:Omit<InputProps,'value'|'onChange'>&{value:string;onPathChange(path:string):void;mode?:'file'|'folder'|'save'}) {
  const t=useT('files'); const {session}=useSession(); const request=useRef<AbortController|null>(null);
  useEffect(()=>()=>request.current?.abort(),[]);
  const browse=async()=>{
    request.current?.abort();const controller=new AbortController();request.current=controller;
    const expanded=value.startsWith('~/')&&session?.home?session.home+value.slice(1):value;
    const title=typeof props.label==='string'?props.label:t('picker.choose');
    try {
      const picked=await requestFilePicker('Ervisio',{mode,title,localOnly:true,initialDirectory:mode==='folder'?expanded||session?.home:expanded?dirname(expanded.replaceAll('\\','/')):session?.home,...(mode==='save'?{initialName:expanded.split(/[\\/]/).at(-1)||''}:{})},controller.signal);
      if(picked&&!controller.signal.aborted)onPathChange(picked.path);
    }catch(error){if(!controller.signal.aborted)toast.err(title,errorMessage(error));}
  };
  return <Input {...props} value={value} onChange={event=>onPathChange(event.target.value)} end={<>{props.end}<IconButton type="button" icon="files" label={`${t('picker.browse')} ${typeof props.label==='string'?props.label:''}`.trim()} onClick={()=>void browse()} disabled={props.disabled}/></>} />;
}
