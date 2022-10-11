package pdf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"reflect"
	"strconv"
	"strings"
)

// PDF 文档结构体, 定义参考：https://cloud.tencent.com/developer/article/1575759
type PDF struct {
	bytes   []byte
	Header  []byte
	Objects []*Obj
	Xref    []*XrefItem
	Trailer *Trailer
}

type Obj struct {
	ID     int // 对象序号
	GenID  int // 生产号
	Dict   []*Pair
	Stream *Stream
	Array  []interface{}
	Int    int
	typ    int
}

func (obj *Obj) IsImageStream() bool {
	// 首先stream对象不为空
	if obj.Stream == nil {
		return false
	}
	// 检查dict中对应的 /Subtype == /Image
	for _, pair := range obj.Dict {
		if pair.Key.Name == "/Subtype" {
			tmp := pair.Value
			value, ok := tmp.(*NameObj)
			if !ok {
				continue
			}
			if value.Name == "/Image" {
				return true
			}
		}
	}
	return false
}

func (obj *Obj) SaveImage(file string) error {
	buf := obj.Stream.body
	if bytes.HasPrefix(buf, []byte("stream")) {
		// 13 + \n
		buf = buf[8:]
	}
	log.Default().Printf("image stream len: %d", len(buf))
	err := os.WriteFile(file, buf, 0666)
	if err != nil {
		log.Default().Fatalf("save file err: %v", err)
		return err
	}
	// save compress
	cfile := fmt.Sprintf("./test-data/cf-%d-%d.jpeg", obj.ID, obj.GenID)
	data := CompressImage(buf)
	return os.WriteFile(cfile, data, 0666)
}

type Pair struct {
	Key   *NameObj
	Value interface{}
}

type NameObj struct {
	Name string
}

type Stream struct {
	body []byte
}

type XrefItem struct {
	ID     int
	Offset int
	GID    int
	Flag   string
}

type Trailer struct {
	Dict      []*Pair
	StartXref int
}

func ReadFromFile(file string) (*PDF, error) {
	bytes, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	log.Default().Printf("read %d bytes", len(bytes))
	p := &PDF{
		bytes: bytes,
	}
	err = p.Parse()
	if err != nil {
		return nil, err
	}
	return p, nil
}

func (p *PDF) ExportJPEG() error {
	for _, obj := range p.Objects {
		if obj.IsImageStream() {
			log.Default().Printf("found a image obj: %d %d", obj.ID, obj.GenID)
			file := fmt.Sprintf("./test-data/%d-%d.jpeg", obj.ID, obj.GenID)
			err := obj.SaveImage(file)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *PDF) compressImageObj() error {
	cnt := 0
	for _, obj := range p.Objects {
		if obj.IsImageStream() {
			cnt++

			filter := p.getNameObjByKey(obj.Dict, "/Filter")
			if filter.Name == "/CCITTFaxDecode" {
				p.compressTIFFObj(obj)
				continue
			}
			// DCTDecode
			buf := obj.Stream.body
			if bytes.HasPrefix(buf, []byte("stream")) {
				// 13 + \n
				buf = buf[8:]
			}
			data := CompressImage(buf)
			start := []byte{'s', 't', 'r', 'e', 'a', 'm', 13, '\n'}
			buf = append(start, data...)
			obj.Stream.body = buf
			// 更新长度
			lenObj := p.getObjRefByKey(obj.Dict, "/Length")
			newLen := len(obj.Stream.body) - 8
			if lenObj != nil {
				p.updateObjLen(lenObj, newLen)
			} else {
				p.updateImageObjLen(obj, newLen)
			}
		}
	}
	log.Default().Printf("compress %d image stream", cnt)
	return nil
}

func (p *PDF) compressTIFFObj(obj *Obj) {
	// 妈的，压缩不了。不处理了
	return
	// 开始处理tiff image
	// https://blog.idrsolutions.com/2011/08/ccitt-encoding-in-pdf-files-converting-pdf-ccitt-data-into-a-tiff/
	parms := p.getDictByKey(obj.Dict, "/DecodeParms")
	// Group 4 Two-Dimensional (G42D): usually have K-values less than 0.
	k := p.getIntByKey(parms, "/K")
	if k >= 0 {
		return
	}
	width := p.getIntByKey(parms, "/Columns")
	height := p.getIntByKey(parms, "/Rows")
	header := []byte{'I', 'I', 42, 0}
	w := bytes.NewBuffer(header)
	//  first_ifd (Image file directory) / offset
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(8))
	w.Write(tmp)

	ifdLength := 10
	headerLength := 10 + (ifdLength*12 + 4)

	tmp = make([]byte, 2)
	binary.LittleEndian.PutUint16(tmp, uint16(ifdLength))
	w.Write(tmp)

	// Dictionary should be in order based on the TiffTag value
	p.writeTIFFTag(w, 255, 4, 1, 0)
	p.writeTIFFTag(w, 256, 4, 1, width)
	p.writeTIFFTag(w, 257, 4, 1, height)
	p.writeTIFFTag(w, 258, 3, 1, 1)
	p.writeTIFFTag(w, 259, 3, 1, 4) //Compression
	p.writeTIFFTag(w, 262, 3, 1, 0)
	p.writeTIFFTag(w, 273, 4, 1, headerLength)
	p.writeTIFFTag(w, 277, 3, 1, 1)
	p.writeTIFFTag(w, 278, 4, 1, height)

	buf := obj.Stream.body
	if bytes.HasPrefix(buf, []byte("stream")) {
		// 13 + \n
		buf = buf[8:]
	}
	p.writeTIFFTag(w, 279, 4, 1, len(buf))

	tmp = make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(0))
	w.Write(tmp)

	w.Write(buf)

	file := fmt.Sprintf("./test-data/tt1-%d-%d.tif", obj.ID, obj.GenID)
	os.WriteFile(file, w.Bytes(), 0666)

	data := CompressTIFFImage(w.Bytes())
	start := []byte{'s', 't', 'r', 'e', 'a', 'm', 13, '\n'}
	buf = append(start, data...)
	obj.Stream.body = buf
	// 更新长度
	lenObj := p.getObjRefByKey(obj.Dict, "/Length")
	newLen := len(obj.Stream.body) - 8
	if lenObj != nil {
		p.updateObjLen(lenObj, newLen)
	} else {
		p.updateImageObjLen(obj, newLen)
	}

}

func (p *PDF) writeTIFFTag(w *bytes.Buffer, tag, typ, count, value int) {
	tmp := make([]byte, 2)
	binary.LittleEndian.PutUint16(tmp, uint16(tag))
	w.Write(tmp)

	tmp = make([]byte, 2)
	binary.LittleEndian.PutUint16(tmp, uint16(typ))
	w.Write(tmp)

	tmp = make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(count))
	w.Write(tmp)

	tmp = make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(value))
	w.Write(tmp)
}

func (p *PDF) updateImageObjLen(obj *Obj, size int) {
	for _, pair := range obj.Dict {
		if pair.Key.Name == "/Length" {
			pair.Value = size
			return
		}
	}
}

func (p *PDF) updateObjLen(obj *Obj, size int) {
	for _, v := range p.Objects {
		if v.ID == obj.ID && v.GenID == obj.GenID {
			v.Int = size
			return
		}
	}
}

func (p *PDF) getDictByKey(dict []*Pair, key string) []*Pair {
	for _, pair := range dict {
		if pair.Key.Name == key {
			list, ok := pair.Value.([]*Pair)
			if ok {
				return list
			}
		}
	}
	return nil
}

func (p *PDF) getIntByKey(dict []*Pair, key string) int {
	for _, pair := range dict {
		if pair.Key.Name == key {
			v, ok := pair.Value.(int)
			if ok {
				return v
			}
		}
	}
	return 0
}

func (p *PDF) getNameObjByKey(dict []*Pair, key string) *NameObj {
	for _, pair := range dict {
		if pair.Key.Name == key {
			name, ok := pair.Value.(*NameObj)
			if ok {
				return name
			}
		}
	}
	return nil
}

func (p *PDF) getObjRefByKey(dict []*Pair, key string) *Obj {
	for _, pair := range dict {
		if pair.Key.Name == key {
			ref, ok := pair.Value.(*Obj)
			if ok {
				return ref
			} else {
				log.Default().Printf("value type: %v, value %v", reflect.TypeOf(pair.Value), pair.Value)
			}
		}
	}
	return nil
}

func (p *PDF) SaveFile(file string, compress bool) error {
	if compress {
		// 更新image object
		err := p.compressImageObj()
		if err != nil {
			return err
		}
	}
	body := make([]byte, 0)
	// 写文件头
	w := bytes.NewBuffer(body)
	w.Write(p.Header)
	w.WriteByte('\n')
	// 写对象集合
	for _, obj := range p.Objects {
		err := p.writeObj(w, obj)
		if err != nil {
			log.Default().Fatalf("write obj err: %v", err)
			return err
		}
	}
	// 写XRef
	err := p.writeXref(w)
	if err != nil {
		return err
	}
	// 写Trailer
	if p.Trailer != nil {
		err := p.writeTrailer(w)
		if err != nil {
			return err
		}
	}
	// 写结束标志
	w.WriteString("%%EOF")

	return os.WriteFile(file, w.Bytes(), 0666)
}

func (p *PDF) writeTrailer(w *bytes.Buffer) error {
	w.WriteString("trailer\n")
	err := p.writeDict(w, p.Trailer.Dict)
	if err != nil {
		return err
	}
	w.WriteString("startref\n")
	str := strconv.Itoa(p.Trailer.StartXref)
	w.WriteString(str)
	w.WriteByte('\n')
	return nil
}

func (p *PDF) writeXref(w *bytes.Buffer) error {
	// 更新trailer中xref的位置
	p.Trailer.StartXref = w.Len()

	w.WriteString("xref\n")
	xref := p.Xref
	for len(xref) > 0 {
		list := p.getXrefSegment(xref)
		if len(list) == 0 {
			return nil
		}
		err := p.writeXrefSegment(w, list)
		if err != nil {
			return err
		}
		xref = xref[len(list):]
	}
	return nil
}

func (p *PDF) writeXrefSegment(w *bytes.Buffer, list []*XrefItem) error {
	str := fmt.Sprintf("%d %d\n", list[0].ID, len(list))
	w.WriteString(str)
	for _, item := range list {
		str := fmt.Sprintf("%010d %05d %s\n", item.Offset, item.GID, item.Flag)
		w.WriteString(str)
	}
	return nil
}

// 获取ID号连续的一段
func (p *PDF) getXrefSegment(list []*XrefItem) []*XrefItem {
	if len(list) == 0 {
		return nil
	}
	id := list[0].ID
	segment := make([]*XrefItem, 0)
	segment = append(segment, list[0])
	for i := 1; i < len(list); i++ {
		if list[i].ID == id+i {
			segment = append(segment, list[i])
			continue
		}
		break
	}
	return segment
}

func (p *PDF) writeObj(w *bytes.Buffer, obj *Obj) error {
	// 更新Xref的位置
	offset := w.Len()
	p.updateXref(obj, offset)
	// 4 0 obj
	start := fmt.Sprintf("%d %d obj", obj.ID, obj.GenID)
	w.WriteString(start)
	w.WriteByte('\n')
	if len(obj.Dict) > 0 {
		// 写字典
		err := p.writeDict(w, obj.Dict)
		if err != nil {
			return err
		}
	}
	if obj.Stream != nil {
		err := p.writeStream(w, obj, obj.Stream)
		if err != nil {
			return err
		}
	}
	if obj.typ == ElementTypeNum {
		str := strconv.Itoa(obj.Int)
		w.WriteString(str)
		w.WriteByte('\n')
	}
	if len(obj.Array) > 0 {
		// 写数组
		err := p.writeArray(w, obj.Array)
		if err != nil {
			return err
		}
		w.WriteByte('\n')
	}
	// end
	w.WriteString("endobj\n")
	return nil
}

func (p *PDF) updateXref(obj *Obj, offset int) {
	for _, item := range p.Xref {
		if item.ID == obj.ID && item.GID == obj.GenID {
			item.Offset = offset
			//log.Default().Printf("update xref for %d %d, offset: %d", obj.ID, obj.GenID, offset)
			return
		}
	}
}

func (p *PDF) writeStream(w *bytes.Buffer, obj *Obj, stream *Stream) error {
	// 如果是image stream
	w.Write(stream.body)
	w.WriteByte('\n')
	w.WriteString("endstream\n")
	return nil
}

func (p *PDF) writeDict(w *bytes.Buffer, dict []*Pair) error {
	// start dict
	w.WriteString("<<\n")
	for _, pair := range dict {
		// 写key
		key := pair.Key.Name
		w.WriteString(key)
		// 写value
		// name 类型
		name, ok := pair.Value.(*NameObj)
		if ok {
			w.WriteByte(' ')
			w.WriteString(name.Name)
			w.WriteByte('\n')
			continue
		}
		// 内嵌的字典类型
		subDict, ok := pair.Value.([]*Pair)
		if ok {
			// 给key 换行
			w.WriteByte('\n')
			err := p.writeDict(w, subDict)
			if err != nil {
				return nil
			}
			continue
		}
		// 对象引用
		obj, ok := pair.Value.(*Obj)
		if ok {
			w.WriteByte(' ')
			err := p.writeObjRef(w, obj)
			if err != nil {
				return nil
			}
			w.WriteByte('\n')
			continue
		}
		// 数组对象
		array, ok := pair.Value.([]interface{})
		if ok {
			w.WriteByte(' ')
			err := p.writeArray(w, array)
			if err != nil {
				return nil
			}
			w.WriteByte('\n')
			continue
		}

		// int类型
		v, ok := pair.Value.(int)
		if ok {
			str := strconv.Itoa(v)
			w.WriteByte(' ')
			w.WriteString(str)
			w.WriteByte('\n')
			continue
		}

		// XString
		str, ok := pair.Value.(string)
		if ok {
			w.WriteByte(' ')
			w.WriteString(str)
			w.WriteByte('\n')
			continue
		}

		typ := reflect.TypeOf(pair.Value)
		log.Default().Fatalf("dict value type: %v, value: %v", typ, pair.Value)
	}
	// end dict
	w.WriteString(">>\n")
	return nil
}

func (p *PDF) writeArray(w *bytes.Buffer, array []interface{}) error {
	w.WriteString("[ ")
	// 写单个数组值
	for i, item := range array {
		v, ok := item.(int)
		if ok {
			str := strconv.Itoa(v)
			w.WriteString(str)
			if i < len(array)-1 {
				w.WriteByte(' ')
			}
			continue
		}
		obj, ok := item.(*Obj)
		if ok {
			p.writeObjRef(w, obj)
			if i < len(array)-1 {
				w.WriteByte(' ')
			}
			continue
		}

		name, ok := item.(*NameObj)
		if ok {
			str := name.Name
			w.WriteString(str)
			if i < len(array)-1 {
				w.WriteByte(' ')
			}
			continue
		}

		str, ok := item.(string)
		if ok {
			w.WriteString(str)
			if i < len(array)-1 {
				w.WriteByte(' ')
			}
			continue
		}

		if !ok {
			log.Default().Fatalf("array item unknown type: %v, value: %v", reflect.TypeOf(item), item)
		}
	}
	w.WriteString(" ]")
	return nil
}

func (p *PDF) writeObjRef(w *bytes.Buffer, obj *Obj) error {
	str := fmt.Sprintf("%d %d R", obj.ID, obj.GenID)
	w.WriteString(str)
	return nil
}

func (p *PDF) Parse() error {
	// 读取文件头
	err := p.readHeader()
	if err != nil {
		return err
	}
	// 读取对象集合
	for {
		typ := p.detectType()
		if typ == ElementTypeObj {
			err = p.readObjects()
			if err != nil {
				return err
			}
			continue
		}

		if typ == ElementTypeXref {
			err = p.readXref()
			if err != nil {
				return err
			}
			continue
		}

		if typ == ElementTypeTrailer {
			err = p.readTrailer()
			if err != nil {
				return err
			}
			continue
		}

		if typ == ElementTypeEOF {
			log.Default().Printf("read file EOF, parse finished")
			break
		}

		log.Default().Fatalf("unknown type %d", typ)
	}

	return nil
}

func (p *PDF) readTrailer() error {
	p.skipSpace()
	str, err := p.readString()
	if err != nil {
		return err
	}
	if str != "trailer" {
		return errors.New("expect trailer")
	}

	dict, err := p.readDict()
	if err != nil {
		return err
	}
	p.skipSpace()
	str, err = p.readString()
	if err != nil {
		return err
	}
	if str != "startxref" {
		return errors.New("expect startxref")
	}
	p.skipSpace()
	offset, err := p.readInt()
	if err != nil {
		return err
	}
	trailer := &Trailer{
		Dict:      dict,
		StartXref: offset,
	}
	p.Trailer = trailer
	return nil
}

func (p *PDF) readXref() error {
	log.Default().Printf("start to read xref")
	start, err := p.readString()
	if err != nil {
		return err
	}
	if start != "xref" {
		return errors.New("expect xref")
	}
	list := make([]*XrefItem, 0)
	for {
		p.skipSpace()
		// 先读两个整数
		id, err := p.readInt()
		if err != nil {
			break
		}
		log.Default().Printf("read start id: %d", id)
		p.skipSpace()
		cnt, err := p.readInt()
		if err != nil {
			return err
		}
		for i := 0; i < cnt; i++ {
			p.skipSpace()
			offset, _ := p.readInt()
			p.skipSpace()
			gid, _ := p.readInt()
			flag, _ := p.readString()
			list = append(list, &XrefItem{
				ID:     id + i,
				Offset: offset,
				GID:    gid,
				Flag:   flag,
			})
		}
	}
	p.Xref = list
	log.Default().Printf("read xref len %d", len(list))
	return nil
}

func (p *PDF) readObjects() error {
	p.skipSpace()
	obj := &Obj{}
	// 对象序号
	id, err := p.readInt()
	if err != nil {
		return err
	}
	obj.ID = id

	// 对象生成号
	gid, err := p.readInt()
	if err != nil {
		return err
	}
	obj.GenID = gid

	// read obj
	str, err := p.readString()
	if err != nil {
		return err
	}
	if str != "obj" {
		return errors.New("expect obj")
	}
	log.Default().Printf("read object start: obj")

	for {
		typ := p.detectType()
		// 开始读取字典对象
		if typ == ElementTypeDict {
			dict, err := p.readDict()
			if err != nil {
				return err
			}
			obj.Dict = dict
			continue
		}

		if typ == ElementTypeCmdEndObj {
			p.readString()
			log.Default().Printf("read object end: endobj")
			break
		}

		if typ == ElementTypeStream {
			stream, err := p.readStream()
			if err != nil {
				return err
			}
			obj.Stream = stream
			continue
		}

		if typ == ElementTypeArray {
			// 数组对象
			array, err := p.readArray()
			if err != nil {
				return err
			}
			obj.Array = array
			continue
		}

		if typ == ElementTypeNum {
			p.skipSpace()
			v, err := p.readInt()
			if err != nil {
				return err
			}
			obj.Int = v
			obj.typ = ElementTypeNum
			continue
		}

		log.Default().Fatalf("unknown typ: %d", typ)
	}

	//log.Default().Printf("read obj[%d %d]: %v", obj.ID, obj.GenID, obj)
	p.Objects = append(p.Objects, obj)
	return nil
}

func (p *PDF) readArray() ([]interface{}, error) {
	p.skipSpace()
	ch := p.bytes[0]
	if ch != '[' {
		return nil, errors.New("expect [")
	}
	p.bytes = p.bytes[1:]
	list := make([]interface{}, 0)
	for {
		typ := p.detectType()
		if typ == ElementTypeName {
			name, err := p.readNameObj()
			if err != nil {
				return nil, err
			}
			str := name.Name
			end := strings.HasSuffix(str, "]")
			if end {
				name.Name = str[:len(str)-1]
			}
			list = append(list, name)
			if end {
				break
			}
			continue
		}
		if typ == ElementTypeNum {
			v, err := p.readInt()
			if err != nil {
				return nil, err
			}
			list = append(list, v)
			continue
		}

		if typ == ElementTypeObjRef {
			obj, err := p.readObjRef()
			if err != nil {
				return nil, err
			}
			list = append(list, obj)
			continue
		}

		if typ == ElementTypeHexString {
			hex, err := p.readHexString()
			if err != nil {
				return nil, err
			}
			list = append(list, hex)
			continue
		}

		if typ == ElementTypeCmdEndArray {
			str, err := p.readString()
			if err != nil {
				return nil, err
			}
			str = str[0 : len(str)-1]
			if len(str) == 0 {
				break
			}
			if p.isNumber(str) {
				v, err := strconv.Atoi(str)
				if err != nil {
					return nil, err
				}
				list = append(list, v)
			} else if strings.HasPrefix(str, "/") {
				name := &NameObj{Name: str}
				list = append(list, name)
			} else {
				log.Default().Fatalf("unknown array end typ: %s", str)
			}
			break
		} else {
			log.Default().Fatalf("unknown typ: %d", typ)
		}
	}
	return list, nil
}

func (p *PDF) readHexString() (string, error) {
	hex, err := p.readString()
	if err != nil {
		return "", err
	}
	// 可能一下子读出了多个字符串，只取第一个
	if !strings.HasPrefix(hex, "<") {
		return "", errors.New("expect hex string: <")
	}
	idx := 0
	for i, ch := range hex {
		b := byte(ch)
		if b == '>' {
			idx = i
			break
		}
	}
	num := hex[0 : idx+1]
	left := hex[idx+1:]
	bytes := make([]byte, 0)
	bytes = append(bytes, left...)
	bytes = append(bytes, p.bytes...)
	p.bytes = bytes
	return num, nil
}

func (p *PDF) readNameObj() (*NameObj, error) {
	str, err := p.readString()
	if err != nil {
		return nil, err
	}
	return &NameObj{Name: str}, nil
}

func (p *PDF) readStream() (*Stream, error) {
	log.Default().Printf("start read stream")
	// 按行读，知道读到 endstream为止
	lines := bytes.Split(p.bytes, []byte("\n"))
	if len(lines) == 0 {
		return nil, errors.New("expect multi lines")
	}
	// 第一行必须为 stream
	line := string(lines[0])
	if strings.HasPrefix(line, "stream") {
		return nil, errors.New("expect stream")
	}
	p.bytes = p.bytes[len(line)+1:]
	lines = lines[1:]
	buf := make([]byte, 0)
	for len(lines) > 0 {
		line := lines[0]
		// 最后一行必须为 endstream
		if len(line) == 9 && string(line) == "endstream" {
			log.Default().Printf("read stream end")
			break
		}
		buf = append(buf, line...)
		buf = append(buf, '\n')
		lines = lines[1:]
	}
	buf = buf[:len(buf)-1]
	p.bytes = p.bytes[len(buf)+1:]
	p.bytes = p.bytes[len("endstream")+1:]
	return &Stream{body: buf}, nil
}

func (p *PDF) readDict() ([]*Pair, error) {
	log.Default().Print("start to read dict")
	// 读取开头的 <<
	start, err := p.readString()
	if err != nil {
		return nil, err
	}
	if start != "<<" {
		return nil, errors.New("expect <<")
	}
	dict := make([]*Pair, 0)
	for {
		typ := p.detectType()
		if typ == ElementTypeCmdEndDict {
			p.readString()
			break
		}
		if typ != ElementTypeName {
			return nil, errors.New("expect /Name object")
		}
		// read key, /Name
		key, err := p.readNameObj()
		if err != nil {
			return nil, err
		}
		item := &Pair{}
		item.Key = key
		// read value
		typ = p.detectType()
		if typ == ElementTypeName {
			value, err := p.readNameObj()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue
		}
		if typ == ElementTypeDict {
			value, err := p.readDict()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue
		}
		if typ == ElementTypeObjRef {
			value, err := p.readObjRef()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue
		}
		if typ == ElementTypeArray {
			value, err := p.readArray()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue

		}

		if typ == ElementTypeNum {
			value, err := p.readInt()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue
		}

		if typ == ElementTypeString {
			value, err := p.readXString()
			if err != nil {
				return nil, err
			}
			item.Value = value
			dict = append(dict, item)
			continue
		}

		log.Default().Fatalf("unknown type: %d", typ)
	}
	return dict, nil
}

func (p *PDF) readXString() (string, error) {
	// 读小括号包围的字符串
	p.skipSpace()
	buf := make([]byte, 0)
	reader := bytes.NewReader(p.bytes)
	cnt := 0
	b, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	cnt++
	for {
		buf = append(buf, b)
		b, err = reader.ReadByte()
		if err != nil {
			return "", err
		}
		cnt++
		if b == ')' {
			buf = append(buf, b)
			break
		}
	}
	p.bytes = p.bytes[cnt:]
	return string(buf), nil
}

const (
	ElementTypeUnkown      = -1
	ElementTypeObj         = 0
	ElementTypeDict        = 1
	ElementTypeName        = 2
	ElementTypeNum         = 3
	ElementTypeObjRef      = 4
	ElementTypeCmdEndObj   = 5
	ElementTypeStream      = 6
	ElementTypeArray       = 7
	ElementTypeCmdEndDict  = 8
	ElementTypeCmdEndArray = 9
	ElementTypeString      = 10
	ElementTypeXref        = 11
	ElementTypeTrailer     = 12
	ElementTypeHexString   = 13
	ElementTypeEOF         = 14
)

func (p *PDF) detectType() int {
	size := 20
	if len(p.bytes) < size {
		size = len(p.bytes)
	}
	// 取前20个字符看一下，可能的类型
	buf := string(p.bytes[0:size])
	buf = strings.ReplaceAll(buf, "\n", " ")
	log.Default().Printf("detect str: %s", buf)
	buf = strings.TrimSpace(buf)
	if len(buf) == 0 {
		return ElementTypeUnkown
	}
	words := strings.Split(buf, " ")
	// 对象
	if len(words) >= 3 {
		// 两个数 + "R"
		if p.isNumber(words[0]) && p.isNumber(words[1]) && words[2] == "R" {
			log.Default().Printf("found type obj ref")
			return ElementTypeObjRef
		}
		// 两个数 + "obj"
		if p.isNumber(words[0]) && p.isNumber(words[1]) && words[2] == "obj" {
			log.Default().Printf("found type obj")
			return ElementTypeObj
		}

	}

	// 数字类型
	if len(words) > 0 {
		if p.isNumber(words[0]) {
			log.Default().Printf("found type number")
			return ElementTypeNum
		}
	}

	// 字典类型 <<
	if len(words) > 0 && words[0] == "<<" {
		log.Default().Printf("found type dict")
		return ElementTypeDict
	}

	// 字典类型 <<
	if len(words) > 0 && words[0] == ">>" {
		log.Default().Printf("found type cmd end dict")
		return ElementTypeCmdEndDict
	}

	// endobj
	if len(words) > 0 && words[0] == "endobj" {
		log.Default().Printf("found type cmd endobj")
		return ElementTypeCmdEndObj
	}

	// stream
	if len(words) > 0 && strings.HasPrefix(words[0], "stream") {
		log.Default().Printf("found type stream")
		return ElementTypeStream
	}

	// 数组
	if len(words) > 0 && strings.HasPrefix(words[0], "[") {
		log.Default().Printf("found type array")
		return ElementTypeArray
	}

	// /Name
	if len(words) > 0 && strings.HasPrefix(words[0], "/") {
		log.Default().Printf("found type name")
		return ElementTypeName
	}
	// end array
	if len(words) > 0 && strings.HasSuffix(words[0], "]") {
		log.Default().Printf("found type cmd end array")
		return ElementTypeCmdEndArray
	}

	// 字符串对象
	if len(words) > 0 && strings.HasPrefix(words[0], "(") {
		log.Default().Printf("found type XString")
		return ElementTypeString
	}

	// 16进制字符串
	if len(words) > 0 && strings.HasPrefix(words[0], "<") && p.isHex(words[0]) {
		log.Default().Printf("found type HexString")
		return ElementTypeHexString
	}

	if len(words) > 0 && words[0] == "xref" {
		log.Default().Printf("found type xref")
		return ElementTypeXref
	}

	if len(words) > 0 && words[0] == "trailer" {
		log.Default().Printf("found type trailer")
		return ElementTypeTrailer
	}

	if len(words) > 0 && words[0] == "%%EOF" {
		log.Default().Printf("found type EOF")
		return ElementTypeEOF
	}

	return -1
}

func (p *PDF) isHex(str string) bool {
	if strings.HasPrefix(str, "<") {
		str = str[1:]
	}
	if strings.HasSuffix(str, ">") {
		str = str[:len(str)-1]
	}
	for _, ch := range str {
		b := byte(ch)
		if b >= '0' && b <= '9' {
			continue
		}
		if b >= 'a' && b <= 'f' {
			continue
		}
		return false
	}
	return true
}

func (p *PDF) isNumber(str string) bool {
	if strings.HasPrefix(str, "-") {
		str = str[1:]
	}
	for _, ch := range str {
		b := byte(ch)
		if b >= '0' && b <= '9' {
			continue
		} else {
			return false
		}
	}
	return true
}

func (p *PDF) readIntArray() ([]int, error) {
	p.skipSpace()
	start, err := p.readString()
	if err != nil {
		return nil, err
	}
	if start != "[" {
		return nil, errors.New("expect [")
	}
	list := make([]int, 0)
	for {
		n, err := p.readInt()
		if err != nil {
			break
		}
		list = append(list, n)
	}
	end, err := p.readString()
	if err != nil {
		return nil, err
	}
	if end != "]" {
		return nil, errors.New("expect ]")
	}
	return list, nil
}

func (p *PDF) readObjRef() (*Obj, error) {
	id, err := p.readInt()
	if err != nil {
		return &Obj{}, nil
	}
	gid, err := p.readInt()
	if err != nil {
		return nil, err
	}
	s, err := p.readString()
	if err != nil {
		return nil, err
	}
	if s != "R" {
		return nil, errors.New("expect R")
	}
	return &Obj{
		ID:    id,
		GenID: gid,
	}, nil
}

func (p *PDF) readString() (string, error) {
	p.skipSpace()
	buf := make([]byte, 0)
	reader := bytes.NewReader(p.bytes)
	b, err := reader.ReadByte()
	if err != nil {
		return "", err
	}
	for b != ' ' && b != '\n' {
		buf = append(buf, b)
		b, err = reader.ReadByte()
		if err != nil {
			return "", err
		}
	}
	p.bytes = p.bytes[len(buf):]
	return string(buf), nil
}

func (p *PDF) skipSpace() {
	reader := bytes.NewReader(p.bytes)
	cnt := 0
	b, err := reader.ReadByte()
	if err != nil {
		return
	}
	cnt++
	for b == ' ' || b == '\n' {
		b, err = reader.ReadByte()
		if err != nil {
			return
		}
		cnt++
	}
	p.bytes = p.bytes[cnt-1:]
}

func (p *PDF) readInt() (int, error) {
	p.skipSpace()
	buf := make([]byte, 0)
	reader := bytes.NewReader(p.bytes)
	cnt := 0
	b, err := reader.ReadByte()
	if err != nil {
		return 0, err
	}
	cnt++
	// 忽略前面的空格
	for b == ' ' {
		b, err = reader.ReadByte()
		if err != nil {
			return 0, err
		}
		cnt++
	}
	for b >= '0' && b <= '9' || b == '-' {
		buf = append(buf, b)
		b, err = reader.ReadByte()
		if err != nil {
			return 0, err
		}
		cnt++
	}
	// 最后一个非数字放回去
	p.bytes = p.bytes[cnt-1:]
	return strconv.Atoi(string(buf))
}

func (p *PDF) readHeader() error {
	// 读取第一行，内容如: %PDF-1.7
	buf := make([]byte, 0)
	reader := bytes.NewReader(p.bytes)
	b, err := reader.ReadByte()
	if err != nil {
		return err
	}
	for b != '\n' {
		buf = append(buf, b)
		b, err = reader.ReadByte()
		if err != nil {
			return err
		}
	}
	p.bytes = p.bytes[len(buf)+1:]
	p.Header = buf
	log.Default().Printf("read header %s", string(buf))
	return nil
}
