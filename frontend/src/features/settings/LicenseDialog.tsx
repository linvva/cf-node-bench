import { Button, Modal, Tabs } from "@heroui/react";
import { Scale, X } from "lucide-react";
import projectLicense from "../../../../LICENSE?raw";
import thirdPartyNotices from "../../../public/THIRD_PARTY_NOTICES.md?raw";
import unicodeLicense from "../../../public/licenses/Unicode-3.0.txt?raw";

const documents=[
  {id:"project",title:"项目许可证",text:projectLicense},
  {id:"third-party",title:"第三方声明",text:thirdPartyNotices},
  {id:"unicode",title:"Unicode 许可",text:unicodeLicense},
];

export function LicenseDialog(){
  return <Modal>
    <Button variant="tertiary" size="sm"><Scale size={14}/>许可证与第三方声明</Button>
    <Modal.Backdrop variant="opaque"><Modal.Container size="lg" scroll="inside"><Modal.Dialog className="license-dialog">
      <Modal.Header><Modal.Heading>许可证与第三方声明</Modal.Heading><Modal.CloseTrigger aria-label="关闭许可证与第三方声明"><X size={17}/></Modal.CloseTrigger></Modal.Header>
      <Modal.Body><Tabs defaultSelectedKey="project" variant="secondary">
        <Tabs.ListContainer><Tabs.List aria-label="许可文档">{documents.map(document=><Tabs.Tab key={document.id} id={document.id}>{document.title}</Tabs.Tab>)}</Tabs.List></Tabs.ListContainer>
        {documents.map(document=><Tabs.Panel key={document.id} id={document.id}><pre className="license-text">{document.text}</pre></Tabs.Panel>)}
      </Tabs></Modal.Body>
    </Modal.Dialog></Modal.Container></Modal.Backdrop>
  </Modal>;
}
